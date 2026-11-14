// Package fieldkeys manages the data keys that seal fields at rest (internal/shared/fieldcrypt):
// the key health card, the backup download and the rotation with its re-seal run. Only the platform
// owner reaches it, and backup and rotation also need a fresh auth-api email code.
//
// FIELD_ENCRYPTION_KEY is the root. The app cannot change its own env, so rotation does not touch
// the root: it creates a data key, stores it sealed under the root in field_keys, makes it current,
// and re-seals every stored value under it. The old key stays loaded for decrypt until the re-seal
// has moved every value and every replica has had time to pick up the new key; then it is retired
// and its material cleared.
package fieldkeys

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/ent"
	"github.com/bengobox/projects-service/internal/ent/fieldkey"
	"github.com/bengobox/projects-service/internal/ent/fieldkeyaudit"
	"github.com/bengobox/projects-service/internal/platform/authotp"
	"github.com/bengobox/projects-service/internal/shared/fieldcrypt"
)

var (
	ErrNotConfigured   = errors.New("FIELD_ENCRYPTION_KEY is not configured")
	ErrOTPRequired     = errors.New("a fresh email code is required")
	ErrOTPInvalid      = errors.New("the email code is not valid, already used, or not for this account")
	ErrOTPUnavailable  = errors.New("email codes are not available right now")
	ErrRotationRunning = errors.New("the last rotation is still re-sealing values")
)

// Rotation states on the health card.
const (
	StateIdle      = "idle"      // never rotated: values are under the root
	StateResealing = "resealing" // a rotation is moving values to the current key
	StateDone      = "done"      // every value is under the current key, older keys retired
)

// Audit actions.
const (
	ActionBackup = "backup"
	ActionRotate = "rotate"
	ActionRetire = "retire"
)

// OTPValidator trades an auth-api otp_token for the address it proved (internal/platform/authotp).
type OTPValidator interface {
	Validate(ctx context.Context, token string) (string, error)
}

// Actor is the signed-in platform owner.
type Actor struct {
	UserID uuid.UUID
	Email  string
}

// target is one column holding sealed values, bound to the row id. Add one here with every new
// sealed column, or rotation will retire a key that column still needs.
type target struct{ name, table, column string }

var targets = []target{
	{name: "qa_invites.email_enc", table: "qa_invites", column: "email_enc"},
}

// Service implements the module.
type Service struct {
	client *ent.Client
	db     *sql.DB
	root   *fieldcrypt.Box
	ring   *fieldcrypt.Ring
	otp    OTPValidator
	log    *zap.Logger

	// grace is how long a new key must have been current before older keys retire, so every
	// replica has reloaded the ring (refreshEvery) and stopped sealing under the old key.
	grace time.Duration
	// staleAfter lets another replica take over a re-seal whose runner stopped sending heartbeats.
	staleAfter time.Duration
	batch      int
	kick       chan struct{}
}

// refreshEvery is how often Run reloads the ring and continues an unfinished re-seal.
const refreshEvery = 30 * time.Second

// New builds the service. root and ring are nil when FIELD_ENCRYPTION_KEY is not set.
func New(client *ent.Client, db *sql.DB, root *fieldcrypt.Box, ring *fieldcrypt.Ring, otp OTPValidator, log *zap.Logger) *Service {
	s := &Service{
		client: client, db: db, root: root, ring: ring, otp: otp, log: log.Named("fieldkeys"),
		grace: 5 * time.Minute, staleAfter: 2 * time.Minute, batch: 200,
		kick: make(chan struct{}, 1),
	}
	if ring != nil {
		ring.SetRefresh(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return s.Load(ctx)
		})
	}
	return s
}

func (s *Service) configured() bool { return s.root != nil && s.ring != nil }

// Load reads the current and previous data keys, opens them with the root and installs them in
// the ring. With no data key the ring seals under the root.
func (s *Service) Load(ctx context.Context) error {
	if !s.configured() {
		return nil
	}
	rows, err := s.client.FieldKey.Query().
		Where(fieldkey.StatusIn(fieldkey.StatusCurrent, fieldkey.StatusPrevious)).All(ctx)
	if err != nil {
		return fmt.Errorf("load field keys: %w", err)
	}
	keys := make(map[string][]byte, len(rows))
	current := ""
	for _, k := range rows {
		raw, err := s.root.OpenDataKey(k.ID, k.KeyEnc)
		if err != nil {
			// Sealed under another root: FIELD_ENCRYPTION_KEY was changed by hand.
			return fmt.Errorf("field key %s does not open under root %s (sealed under %s): %w",
				k.ID, s.root.Fingerprint(), k.RootFingerprint, err)
		}
		keys[k.ID] = raw
		if k.Status == fieldkey.StatusCurrent {
			current = k.ID
		}
	}
	return s.ring.Install(current, keys)
}

// verifyOTP checks that token is a fresh auth-api email code proven by the actor's own address.
func (s *Service) verifyOTP(ctx context.Context, actor Actor, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrOTPRequired
	}
	if s.otp == nil {
		return ErrOTPUnavailable
	}
	email, err := s.otp.Validate(ctx, token)
	if errors.Is(err, authotp.ErrNotConfigured) {
		return ErrOTPUnavailable
	}
	if err != nil {
		return ErrOTPInvalid
	}
	if strings.TrimSpace(actor.Email) == "" || !strings.EqualFold(strings.TrimSpace(email), strings.TrimSpace(actor.Email)) {
		return ErrOTPInvalid
	}
	return nil
}

func (s *Service) audit(ctx context.Context, c *ent.Client, actor Actor, action, keyID, fp, detail string) error {
	q := c.FieldKeyAudit.Create().SetAction(action).SetKeyID(keyID).SetFingerprint(fp).
		SetRootFingerprint(s.root.Fingerprint()).SetDetail(detail)
	if actor.UserID != uuid.Nil {
		q = q.SetActorID(actor.UserID)
	}
	if actor.Email != "" {
		q = q.SetActorEmail(actor.Email)
	}
	return q.Exec(ctx)
}

// KeyInfo is one data key on the health card.
type KeyInfo struct {
	ID          string     `json:"id"`
	Fingerprint string     `json:"fingerprint"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	RetiredAt   *time.Time `json:"retired_at,omitempty"`
	Values      int        `json:"values"`
}

// Rotation is the progress of the latest rotation.
type Rotation struct {
	State       string     `json:"state"`
	KeyID       string     `json:"key_id,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	HeartbeatAt *time.Time `json:"heartbeat_at,omitempty"`
	Resealed    int        `json:"resealed"`
	Failed      int        `json:"failed"`
	Remaining   int        `json:"remaining"`
}

// Health is the key health card. It names keys by id and fingerprint, never by material.
type Health struct {
	Configured         bool       `json:"configured"`
	RootFingerprint    string     `json:"root_fingerprint,omitempty"`
	CurrentKeyID       string     `json:"current_key_id,omitempty"`
	CurrentFingerprint string     `json:"current_fingerprint,omitempty"`
	SealedValues       int        `json:"sealed_values"`
	OnCurrent          int        `json:"on_current"`
	OnOlder            int        `json:"on_older"`
	OnRoot             int        `json:"on_root"`
	Keys               []KeyInfo  `json:"keys"`
	LastBackupAt       *time.Time `json:"last_backup_at,omitempty"`
	LastBackupBy       string     `json:"last_backup_by,omitempty"`
	LastRotatedAt      *time.Time `json:"last_rotated_at,omitempty"`
	Rotation           Rotation   `json:"rotation"`
}

// countByKey counts the stored sealed values by data key id; "" is the root.
func (s *Service) countByKey(ctx context.Context) (map[string]int, int, error) {
	out := map[string]int{}
	total := 0
	for _, t := range targets {
		q := fmt.Sprintf(`SELECT CASE WHEN %[1]s LIKE 'v3:%%' THEN split_part(%[1]s, ':', 2) ELSE '' END, count(*)
			FROM %[2]s WHERE coalesce(%[1]s, '') <> '' GROUP BY 1`, t.column, t.table)
		rows, err := s.db.QueryContext(ctx, q)
		if err != nil {
			return nil, 0, fmt.Errorf("count %s: %w", t.name, err)
		}
		for rows.Next() {
			var id string
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				_ = rows.Close()
				return nil, 0, err
			}
			out[id] += n
			total += n
		}
		if err := rows.Close(); err != nil {
			return nil, 0, err
		}
		if err := rows.Err(); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

// Health builds the key health card.
func (s *Service) Health(ctx context.Context) (Health, error) {
	h := Health{Keys: []KeyInfo{}, Rotation: Rotation{State: StateIdle}}
	if !s.configured() {
		return h, nil
	}
	h.Configured = true
	h.RootFingerprint = s.root.Fingerprint()

	counts, total, err := s.countByKey(ctx)
	if err != nil {
		return h, err
	}
	h.SealedValues = total
	h.OnRoot = counts[""]

	keys, err := s.client.FieldKey.Query().Order(ent.Desc(fieldkey.FieldCreatedAt)).Limit(20).All(ctx)
	if err != nil {
		return h, err
	}
	for _, k := range keys {
		h.Keys = append(h.Keys, KeyInfo{ID: k.ID, Fingerprint: k.Fingerprint, Status: string(k.Status),
			CreatedAt: k.CreatedAt, RetiredAt: k.RetiredAt, Values: counts[k.ID]})
		if k.Status != fieldkey.StatusCurrent {
			continue
		}
		created := k.CreatedAt
		h.CurrentKeyID, h.CurrentFingerprint, h.LastRotatedAt = k.ID, k.Fingerprint, &created
		h.Rotation = Rotation{State: StateResealing, KeyID: k.ID, StartedAt: &created, FinishedAt: k.ResealDoneAt,
			HeartbeatAt: k.ResealHeartbeatAt, Resealed: k.Resealed, Failed: k.ResealFailed}
		if k.ResealDoneAt != nil {
			h.Rotation.State = StateDone
		}
	}
	h.OnCurrent = counts[h.CurrentKeyID]
	h.OnOlder = total - h.OnCurrent
	h.Rotation.Remaining = h.OnOlder

	if last, err := s.client.FieldKeyAudit.Query().Where(fieldkeyaudit.Action(ActionBackup)).
		Order(ent.Desc(fieldkeyaudit.FieldCreatedAt)).First(ctx); err == nil {
		at := last.CreatedAt
		h.LastBackupAt, h.LastBackupBy = &at, last.ActorEmail
	} else if !ent.IsNotFound(err) {
		return h, err
	}
	return h, nil
}

// Rotate makes a new current data key and asks the runner to re-seal every value under it. It
// needs the actor's fresh email code, and refuses while the last rotation is still re-sealing.
func (s *Service) Rotate(ctx context.Context, actor Actor, otpToken string) (Health, error) {
	if !s.configured() {
		return Health{}, ErrNotConfigured
	}
	if running, err := s.rotationRunning(ctx, s.client); err != nil {
		return Health{}, err
	} else if running {
		return Health{}, ErrRotationRunning
	}
	if err := s.verifyOTP(ctx, actor, otpToken); err != nil {
		return Health{}, err
	}
	id, raw, err := fieldcrypt.NewDataKey()
	if err != nil {
		return Health{}, err
	}
	sealed, err := s.root.SealDataKey(id, raw)
	if err != nil {
		return Health{}, err
	}
	fp := fieldcrypt.Fingerprint(raw)

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return Health{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if running, err := s.rotationRunning(ctx, tx.Client()); err != nil {
		return Health{}, err
	} else if running {
		return Health{}, ErrRotationRunning
	}
	if err := tx.FieldKey.Update().Where(fieldkey.StatusEQ(fieldkey.StatusCurrent)).
		SetStatus(fieldkey.StatusPrevious).Exec(ctx); err != nil {
		return Health{}, err
	}
	c := tx.FieldKey.Create().SetID(id).SetKeyEnc(sealed).SetFingerprint(fp).
		SetRootFingerprint(s.root.Fingerprint()).SetStatus(fieldkey.StatusCurrent)
	if actor.UserID != uuid.Nil {
		c = c.SetCreatedBy(actor.UserID)
	}
	if err := c.Exec(ctx); err != nil {
		return Health{}, err
	}
	if err := s.audit(ctx, tx.Client(), actor, ActionRotate, id, fp, "new data key; re-seal started"); err != nil {
		return Health{}, err
	}
	if err := tx.Commit(); err != nil {
		return Health{}, err
	}
	s.log.Info("field key rotated", zap.String("key_id", id), zap.String("fingerprint", fp))
	if err := s.Load(ctx); err != nil {
		s.log.Error("field keys not reloaded after rotation", zap.Error(err))
	}
	select {
	case s.kick <- struct{}{}:
	default:
	}
	return s.Health(ctx)
}

func (s *Service) rotationRunning(ctx context.Context, c *ent.Client) (bool, error) {
	return c.FieldKey.Query().Where(fieldkey.StatusEQ(fieldkey.StatusCurrent), fieldkey.ResealDoneAtIsNil()).Exist(ctx)
}

// Report counts one re-seal pass.
type Report struct {
	Moved   int  // values moved to the current key
	Failed  int  // values that did not open (logged by row id) or changed under the pass
	Done    bool // every value is under the current key and older keys are retired
	Skipped bool // another replica holds the re-seal
}

// ResealPass moves every stored value to the current key, batched. It is resumable without a
// cursor (a moved value drops out of the next scan) and idempotent; each row is written only if it
// still holds the value that was read. When nothing is left on an older key, nothing failed and
// the grace has passed, the previous keys are retired. One replica runs it at a time: it claims
// the current key with a heartbeat that another replica may take over once stale.
func (s *Service) ResealPass(ctx context.Context) (Report, error) {
	var rep Report
	if !s.configured() {
		return rep, nil
	}
	cur, err := s.client.FieldKey.Query().Where(fieldkey.StatusEQ(fieldkey.StatusCurrent)).Only(ctx)
	if ent.IsNotFound(err) {
		return rep, nil
	}
	if err != nil {
		return rep, err
	}
	if cur.ResealDoneAt != nil {
		rep.Done = true
		return rep, nil
	}
	claimed, err := s.client.FieldKey.Update().
		Where(fieldkey.ID(cur.ID), fieldkey.StatusEQ(fieldkey.StatusCurrent), fieldkey.ResealDoneAtIsNil(),
			fieldkey.Or(fieldkey.ResealHeartbeatAtIsNil(), fieldkey.ResealHeartbeatAtLT(time.Now().Add(-s.staleAfter)))).
		SetResealHeartbeatAt(time.Now()).Save(ctx)
	if err != nil {
		return rep, err
	}
	if claimed == 0 {
		rep.Skipped = true
		return rep, nil
	}
	defer func() {
		_ = s.client.FieldKey.UpdateOneID(cur.ID).ClearResealHeartbeatAt().Exec(context.WithoutCancel(ctx))
	}()
	if s.ring.CurrentKeyID() != cur.ID {
		if err := s.Load(ctx); err != nil {
			return rep, err
		}
	}
	prefix := s.ring.CurrentPrefix()

	for _, t := range targets {
		cursor := uuid.Nil.String()
		for {
			last, read, moved, failed, err := s.resealBatch(ctx, t, cursor, prefix)
			rep.Moved += moved
			rep.Failed += failed
			if err != nil {
				return rep, fmt.Errorf("reseal %s: %w", t.name, err)
			}
			if moved > 0 {
				if err := s.client.FieldKey.UpdateOneID(cur.ID).AddResealed(moved).SetResealHeartbeatAt(time.Now()).Exec(ctx); err != nil {
					return rep, err
				}
			}
			if read < s.batch {
				break
			}
			cursor = last
		}
	}
	if err := s.client.FieldKey.UpdateOneID(cur.ID).SetResealFailed(rep.Failed).Exec(ctx); err != nil {
		return rep, err
	}

	counts, total, err := s.countByKey(ctx)
	if err != nil {
		return rep, err
	}
	remaining := total - counts[cur.ID]
	if rep.Failed > 0 || remaining > 0 || time.Since(cur.CreatedAt) < s.grace {
		return rep, nil
	}
	if err := s.retirePrevious(ctx, cur.ID); err != nil {
		return rep, err
	}
	rep.Done = true
	return rep, nil
}

func (s *Service) resealBatch(ctx context.Context, t target, cursor, prefix string) (last string, read, moved, failed int, err error) {
	sel := fmt.Sprintf(`SELECT id::text, %[1]s FROM %[2]s WHERE id > $1::uuid AND coalesce(%[1]s, '') <> ''
		AND left(%[1]s, $3) <> $2 ORDER BY id LIMIT $4`, t.column, t.table)
	upd := fmt.Sprintf(`UPDATE %[2]s SET %[1]s = $2 WHERE id = $1::uuid AND %[1]s = $3`, t.column, t.table)
	n := len(prefix)
	if n == 0 {
		// Before the first rotation nothing is re-sealed; ResealPass never gets here then.
		return cursor, 0, 0, 0, nil
	}
	rows, err := s.db.QueryContext(ctx, sel, cursor, prefix, n, s.batch)
	if err != nil {
		return cursor, 0, 0, 0, err
	}
	type pair struct{ id, enc string }
	var found []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.id, &p.enc); err != nil {
			_ = rows.Close()
			return cursor, 0, 0, 0, err
		}
		found = append(found, p)
	}
	if err := rows.Close(); err != nil {
		return cursor, 0, 0, 0, err
	}
	if err := rows.Err(); err != nil {
		return cursor, 0, 0, 0, err
	}
	last = cursor
	for _, p := range found {
		last = p.id
		next, err := s.ring.Reseal(p.enc, p.id)
		if err != nil {
			failed++
			s.log.Warn("sealed value not re-sealed", zap.String("field", t.name), zap.String("row", p.id), zap.Error(err))
			continue
		}
		res, err := s.db.ExecContext(ctx, upd, p.id, next, p.enc)
		if err != nil {
			return last, len(found), moved, failed, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			moved++
		} else {
			failed++ // changed under the pass; the next pass picks it up
		}
	}
	return last, len(found), moved, failed, nil
}

// retirePrevious clears the material of every previous key and marks the rotation done.
func (s *Service) retirePrevious(ctx context.Context, currentID string) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	prev, err := tx.FieldKey.Query().Where(fieldkey.StatusEQ(fieldkey.StatusPrevious)).All(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, k := range prev {
		if err := tx.FieldKey.UpdateOneID(k.ID).SetStatus(fieldkey.StatusRetired).SetKeyEnc("").SetRetiredAt(now).Exec(ctx); err != nil {
			return err
		}
		if err := s.audit(ctx, tx.Client(), Actor{}, ActionRetire, k.ID, k.Fingerprint, "every value re-sealed under "+currentID); err != nil {
			return err
		}
	}
	if err := tx.FieldKey.UpdateOneID(currentID).SetResealDoneAt(now).Exec(ctx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.log.Info("field key rotation finished", zap.String("key_id", currentID), zap.Int("retired", len(prev)))
	return s.Load(ctx)
}

// Run reloads the ring and continues an unfinished re-seal every refreshEvery, and at once when
// this replica starts a rotation. It never starts a rotation itself: only the owner's Rotate does.
func (s *Service) Run(ctx context.Context) {
	if !s.configured() {
		return
	}
	t := time.NewTicker(refreshEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.Load(ctx); err != nil {
				s.log.Warn("field keys not reloaded", zap.Error(err))
			}
		case <-s.kick:
		}
		rep, err := s.ResealPass(ctx)
		if err != nil {
			s.log.Error("field key re-seal pass failed", zap.Error(err))
			continue
		}
		if rep.Moved > 0 || rep.Failed > 0 {
			s.log.Info("field key re-seal pass", zap.Int("moved", rep.Moved), zap.Int("failed", rep.Failed), zap.Bool("done", rep.Done))
		}
	}
}

// BackupKey is one data key in the backup file.
type BackupKey struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
	Key         string    `json:"key"` // base64 of the 32 raw bytes
}

// BackupDocument is the backup file.
type BackupDocument struct {
	Kind            string      `json:"kind"`
	Format          int         `json:"format"`
	CreatedAt       time.Time   `json:"created_at"`
	CreatedBy       string      `json:"created_by"`
	RootFingerprint string      `json:"root_fingerprint"`
	CurrentKeyID    string      `json:"current_key_id"`
	Keys            []BackupKey `json:"keys"`
	ValueFormat     string      `json:"value_format"`
	Instructions    string      `json:"instructions"`
}

// BackupFile is the download.
type BackupFile struct {
	Name string
	Body []byte
}

const backupInstructions = "This file holds the projects-api field encryption data keys in the clear. Keep it offline " +
	"and treat it like a password. It does not hold the root key FIELD_ENCRYPTION_KEY (the projects-api-secrets " +
	"secret, named here by root_fingerprint); back that up on its own. With these keys you can open every value " +
	"sealed under them even if the field_keys table or the root is lost. Values from before the first rotation " +
	"(starting v2: or with no prefix) and the sealed copies in field_keys open only with the root. To restore " +
	"after losing field_keys, insert each key again sealed under the current root."

const valueFormat = "v3:<key id>:base64(12 byte nonce | AES-256-GCM ciphertext and tag); the AES key is the 32 " +
	"decoded bytes of keys[].key and the additional data is the row id (qa_invites.id for qa_invites.email_enc)."

// Backup returns the data keys as a file, once per fresh email code, and records who took it.
func (s *Service) Backup(ctx context.Context, actor Actor, otpToken string) (*BackupFile, error) {
	if !s.configured() {
		return nil, ErrNotConfigured
	}
	if err := s.verifyOTP(ctx, actor, otpToken); err != nil {
		return nil, err
	}
	rows, err := s.client.FieldKey.Query().
		Where(fieldkey.StatusIn(fieldkey.StatusCurrent, fieldkey.StatusPrevious)).
		Order(ent.Desc(fieldkey.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	doc := BackupDocument{
		Kind: "projects-api field encryption keys", Format: 1, CreatedAt: now, CreatedBy: actor.Email,
		RootFingerprint: s.root.Fingerprint(), Keys: []BackupKey{}, ValueFormat: valueFormat,
		Instructions: backupInstructions,
	}
	fp := s.root.Fingerprint()
	for _, k := range rows {
		raw, err := s.root.OpenDataKey(k.ID, k.KeyEnc)
		if err != nil {
			return nil, fmt.Errorf("field key %s does not open: %w", k.ID, err)
		}
		doc.Keys = append(doc.Keys, BackupKey{ID: k.ID, Status: string(k.Status), Fingerprint: k.Fingerprint,
			CreatedAt: k.CreatedAt, Key: base64.StdEncoding.EncodeToString(raw)})
		if k.Status == fieldkey.StatusCurrent {
			doc.CurrentKeyID, fp = k.ID, k.Fingerprint
		}
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	// No file leaves without its audit row.
	if err := s.audit(ctx, s.client, actor, ActionBackup, doc.CurrentKeyID, fp,
		fmt.Sprintf("%d data keys downloaded", len(doc.Keys))); err != nil {
		return nil, err
	}
	s.log.Info("field keys backed up", zap.String("by", actor.UserID.String()), zap.String("fingerprint", fp))
	name := fmt.Sprintf("projects-api-field-keys-%s-%s.json", s.root.Fingerprint(), now.Format("20060102-150405"))
	return &BackupFile{Name: name, Body: body}, nil
}
