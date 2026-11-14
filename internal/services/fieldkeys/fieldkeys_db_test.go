package fieldkeys

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/fieldkey"
	"github.com/bengobox/projects-service/internal/ent/fieldkeyaudit"
	"github.com/bengobox/projects-service/internal/shared/fieldcrypt"
	"github.com/bengobox/projects-service/internal/testutil"
)

// testRootSecret is a fixed, obviously fake root: base64 of a repeated hex alphabet.
var testRootSecret = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("0123456789abcdef", 2)))

// fakeOTP answers like auth-api's S2S validate: one address per token, each token once.
type fakeOTP struct{ tokens map[string]string }

func (f *fakeOTP) Validate(_ context.Context, token string) (string, error) {
	email, ok := f.tokens[token]
	if !ok {
		return "", errors.New("authotp: token invalid or used")
	}
	delete(f.tokens, token)
	return email, nil
}

type fixture struct {
	svc   *Service
	ring  *fieldcrypt.Ring
	root  *fieldcrypt.Box
	otp   *fakeOTP
	ent   *ent.Client
	owner Actor
	ids   map[string]uuid.UUID // plain address -> invite id
}

func setup(t *testing.T) *fixture {
	t.Helper()
	client, db := testutil.ClientDB(t)
	root, err := fieldcrypt.New(testRootSecret)
	if err != nil {
		t.Fatal(err)
	}
	ring := fieldcrypt.NewRing(root)
	otp := &fakeOTP{tokens: map[string]string{}}
	svc := New(client, db, root, ring, otp, zap.NewNop())
	svc.grace = 0
	f := &fixture{svc: svc, ring: ring, root: root, otp: otp, ent: client,
		owner: Actor{UserID: uuid.New(), Email: "Owner@Codevertex.test"}, ids: map[string]uuid.UUID{}}

	ctx := context.Background()
	add := func(email string, seal func(id uuid.UUID) string) {
		id := uuid.New()
		c := client.QAInvite.Create().SetID(id).SetTenantID(uuid.New()).SetSessionID(uuid.New()).
			SetEmailHash(strings.Repeat("a", 64)).SetRole("signer")
		if seal != nil {
			c = c.SetEmailEnc(seal(id))
		}
		if _, err := c.Save(ctx); err != nil {
			t.Fatal(err)
		}
		f.ids[email] = id
	}
	add("v2@x.test", func(id uuid.UUID) string { s, _ := root.EncryptFor("v2@x.test", id.String()); return s })
	add("legacy@x.test", func(uuid.UUID) string { s, _ := root.Encrypt("legacy@x.test"); return s })
	add("tester@x.test", nil)
	return f
}

func (f *fixture) token(email string) string {
	tok := uuid.NewString()
	f.otp.tokens[tok] = email
	return tok
}

func (f *fixture) sealed(t *testing.T, email string) string {
	t.Helper()
	inv, err := f.ent.QAInvite.Get(context.Background(), f.ids[email])
	if err != nil {
		t.Fatal(err)
	}
	return inv.EmailEnc
}

func (f *fixture) opens(t *testing.T, ring *fieldcrypt.Ring, email string) {
	t.Helper()
	got, err := ring.DecryptFor(f.sealed(t, email), f.ids[email].String())
	if err != nil || got != email {
		t.Fatalf("%s does not open: %q %v", email, got, err)
	}
}

func TestHealthBeforeAnyRotation(t *testing.T) {
	f := setup(t)
	h, err := f.svc.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !h.Configured || h.RootFingerprint != f.root.Fingerprint() || h.CurrentKeyID != "" {
		t.Fatalf("health: %+v", h)
	}
	if h.SealedValues != 2 || h.OnCurrent != 2 || h.OnOlder != 0 {
		t.Fatalf("before a rotation the root is current: %+v", h)
	}
	if h.LastBackupAt != nil || h.LastRotatedAt != nil || h.Rotation.State != StateIdle {
		t.Fatalf("nothing happened yet: %+v", h)
	}
}

func TestRotateNeedsTheOwnersFreshCode(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.svc.Rotate(ctx, f.owner, ""); !errors.Is(err, ErrOTPRequired) {
		t.Fatalf("no code: %v", err)
	}
	if _, err := f.svc.Rotate(ctx, f.owner, "made-up"); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("unknown code: %v", err)
	}
	if _, err := f.svc.Rotate(ctx, f.owner, f.token("someone@else.test")); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("a code proving another address: %v", err)
	}
	if n := f.ent.FieldKey.Query().CountX(ctx); n != 0 {
		t.Fatalf("a refused rotation made %d keys", n)
	}
	tok := f.token("owner@codevertex.test")
	if _, err := f.svc.Rotate(ctx, f.owner, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Rotate(ctx, f.owner, tok); !errors.Is(err, ErrOTPInvalid) && !errors.Is(err, ErrRotationRunning) {
		t.Fatalf("a used code must not work twice: %v", err)
	}
}

func TestRotationResealsEverythingThenRetiresTheOldKey(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	h, err := f.svc.Rotate(ctx, f.owner, f.token("owner@codevertex.test"))
	if err != nil {
		t.Fatal(err)
	}
	k1 := h.CurrentKeyID
	if k1 == "" || h.Rotation.State != StateResealing || h.OnOlder != 2 || h.OnCurrent != 0 {
		t.Fatalf("after rotate: %+v", h)
	}
	if a := f.ent.FieldKeyAudit.Query().Where(fieldkeyaudit.Action("rotate")).OnlyX(ctx); a.KeyID != k1 || a.ActorEmail != f.owner.Email {
		t.Fatalf("rotate audit: %+v", a)
	}
	// New values go under the new key straight away.
	if enc, _ := f.ring.EncryptFor("new@x.test", "r"); !strings.HasPrefix(enc, "v3:"+k1+":") {
		t.Fatalf("not sealed under the new key: %q", enc[:8])
	}

	rep, err := f.svc.ResealPass(ctx)
	if err != nil || rep.Moved != 2 || rep.Failed != 0 || !rep.Done {
		t.Fatalf("reseal: %+v %v", rep, err)
	}
	for _, e := range []string{"v2@x.test", "legacy@x.test"} {
		if fieldcrypt.KeyIDOf(f.sealed(t, e)) != k1 {
			t.Fatalf("%s not moved", e)
		}
		f.opens(t, f.ring, e)
	}
	h, _ = f.svc.Health(ctx)
	if h.OnCurrent != 2 || h.OnOlder != 0 || h.Rotation.State != StateDone || h.LastRotatedAt == nil {
		t.Fatalf("after reseal: %+v", h)
	}

	// Second rotation: k1 stays for decrypt until the re-seal is done and the grace has passed.
	f.svc.grace = time.Hour
	h, err = f.svc.Rotate(ctx, f.owner, f.token("owner@codevertex.test"))
	if err != nil {
		t.Fatal(err)
	}
	k2 := h.CurrentKeyID
	if _, err := f.svc.Rotate(ctx, f.owner, f.token("owner@codevertex.test")); !errors.Is(err, ErrRotationRunning) {
		t.Fatalf("a second rotation while one runs: %v", err)
	}
	rep, err = f.svc.ResealPass(ctx)
	if err != nil || rep.Moved != 2 || rep.Done {
		t.Fatalf("reseal inside the grace: %+v %v", rep, err)
	}
	if k := f.ent.FieldKey.GetX(ctx, k1); k.Status != fieldkey.StatusPrevious || k.KeyEnc == "" {
		t.Fatalf("k1 retired inside the grace: %+v", k.Status)
	}

	// Another replica loads both keys from the database and opens the moved values.
	other := fieldcrypt.NewRing(f.root)
	otherSvc := New(f.ent, f.svc.db, f.root, other, f.otp, zap.NewNop())
	if err := otherSvc.Load(ctx); err != nil {
		t.Fatal(err)
	}
	f.opens(t, other, "v2@x.test")

	f.svc.grace = 0
	rep, err = f.svc.ResealPass(ctx)
	if err != nil || rep.Moved != 0 || !rep.Done {
		t.Fatalf("final pass: %+v %v", rep, err)
	}
	k := f.ent.FieldKey.GetX(ctx, k1)
	if k.Status != fieldkey.StatusRetired || k.KeyEnc != "" || k.RetiredAt == nil {
		t.Fatalf("k1 not retired: %v", k.Status)
	}
	if n := f.ent.FieldKeyAudit.Query().Where(fieldkeyaudit.Action("retire"), fieldkeyaudit.KeyID(k1)).CountX(ctx); n != 1 {
		t.Fatalf("retire audit rows: %d", n)
	}
	// After a reload the ring holds only k2 and everything still opens.
	if err := f.svc.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if f.ring.CurrentKeyID() != k2 {
		t.Fatal("current key after reload")
	}
	f.opens(t, f.ring, "v2@x.test")
	f.opens(t, f.ring, "legacy@x.test")
	h, _ = f.svc.Health(ctx)
	if h.Rotation.State != StateDone || len(h.Keys) != 2 {
		t.Fatalf("final health: %+v", h)
	}
}

func TestResealClaimIsExclusive(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.svc.Rotate(ctx, f.owner, f.token("owner@codevertex.test")); err != nil {
		t.Fatal(err)
	}
	cur := f.ent.FieldKey.Query().Where(fieldkey.StatusEQ(fieldkey.StatusCurrent)).OnlyX(ctx)
	f.ent.FieldKey.UpdateOneID(cur.ID).SetResealHeartbeatAt(time.Now()).ExecX(ctx)
	rep, err := f.svc.ResealPass(ctx)
	if err != nil || !rep.Skipped || rep.Moved != 0 {
		t.Fatalf("a pass ran while another replica held the claim: %+v %v", rep, err)
	}
	f.ent.FieldKey.UpdateOneID(cur.ID).SetResealHeartbeatAt(time.Now().Add(-10 * time.Minute)).ExecX(ctx)
	if rep, err := f.svc.ResealPass(ctx); err != nil || rep.Moved != 2 {
		t.Fatalf("a stale claim was not taken over: %+v %v", rep, err)
	}
}

func TestBackupHoldsTheDataKeysButNotTheRoot(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.svc.Backup(ctx, f.owner, f.token("intruder@x.test")); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("backup with another address's code: %v", err)
	}
	if _, err := f.svc.Rotate(ctx, f.owner, f.token("owner@codevertex.test")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ResealPass(ctx); err != nil {
		t.Fatal(err)
	}
	file, err := f.svc.Backup(ctx, f.owner, f.token("owner@codevertex.test"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(file.Name, ".json") || !strings.Contains(file.Name, f.root.Fingerprint()) {
		t.Fatalf("file name %q", file.Name)
	}
	body := string(file.Body)
	if strings.Contains(body, testRootSecret) {
		t.Fatal("the backup holds the root key")
	}
	var doc BackupDocument
	if err := json.Unmarshal(file.Body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.RootFingerprint != f.root.Fingerprint() || len(doc.Keys) != 1 || doc.CurrentKeyID != doc.Keys[0].ID || doc.Instructions == "" {
		t.Fatalf("backup document: %+v", doc)
	}
	// The key in the file opens a stored value by itself, as the file says.
	raw, err := base64.StdEncoding.DecodeString(doc.Keys[0].Key)
	if err != nil || fieldcrypt.Fingerprint(raw) != doc.Keys[0].Fingerprint {
		t.Fatal("key material does not match its fingerprint")
	}
	enc := f.sealed(t, "v2@x.test")
	b64 := enc[len("v3:"+doc.Keys[0].ID+":"):]
	ct, _ := base64.StdEncoding.DecodeString(b64)
	block, _ := aes.NewCipher(raw)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, ct[:12], ct[12:], []byte(f.ids["v2@x.test"].String()))
	if err != nil || string(plain) != "v2@x.test" {
		t.Fatalf("the backed-up key does not open a value: %v", err)
	}

	a := f.ent.FieldKeyAudit.Query().Where(fieldkeyaudit.Action("backup")).OnlyX(ctx)
	if a.ActorEmail != f.owner.Email || a.Fingerprint != doc.Keys[0].Fingerprint || a.RootFingerprint != f.root.Fingerprint() {
		t.Fatalf("backup audit: %+v", a)
	}
	if h, _ := f.svc.Health(ctx); h.LastBackupAt == nil || h.LastBackupBy != f.owner.Email {
		t.Fatalf("health after backup: %+v", h)
	}
}

func TestNotConfigured(t *testing.T) {
	client, db := testutil.ClientDB(t)
	svc := New(client, db, nil, nil, &fakeOTP{}, zap.NewNop())
	h, err := svc.Health(context.Background())
	if err != nil || h.Configured {
		t.Fatalf("health without a root: %+v %v", h, err)
	}
	if _, err := svc.Rotate(context.Background(), Actor{Email: "a@b.c"}, "t"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("rotate without a root: %v", err)
	}
}
