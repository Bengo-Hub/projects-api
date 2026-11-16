package tenders

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/projects-service/internal/testutil"
)

// Tender numbers come from the tenant's counter: every tenant starts at 1, a deleted tender never
// frees its number, and concurrent creates never share one.
func TestTenderNumbering(t *testing.T) {
	client := testutil.Client(t)
	ctx := context.Background()
	svc := NewService(client, nil, zap.NewNop())
	tenantA, tenantB := uuid.New(), uuid.New()
	year := time.Now().Year()
	in := CreateTenderInput{Title: "Road", ClientName: "County"}

	first, err := svc.CreateTender(ctx, tenantA, in)
	if err != nil {
		t.Fatal(err)
	}
	if first.Number != TenderNumber(year, 1) {
		t.Fatalf("first number = %s", first.Number)
	}
	// Another tenant's first tender is also ...-0001 (numbers are unique per tenant only).
	otherFirst, err := svc.CreateTender(ctx, tenantB, in)
	if err != nil {
		t.Fatal(err)
	}
	if otherFirst.Number != TenderNumber(year, 1) {
		t.Fatalf("other tenant's first number = %s", otherFirst.Number)
	}
	if err := svc.DeleteTender(ctx, tenantA, first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateTender(ctx, tenantA, in)
	if err != nil {
		t.Fatal(err)
	}
	if second.Number != TenderNumber(year, 2) {
		t.Fatalf("after a delete the next number = %s, want ...-0002 (COUNT+1 would repeat 0001)", second.Number)
	}

	const n = 8
	var wg sync.WaitGroup
	numbers := make(chan string, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tnd, err := svc.CreateTender(ctx, tenantA, in)
			if err != nil {
				errs <- err
				return
			}
			numbers <- tnd.Number
		}()
	}
	wg.Wait()
	close(numbers)
	close(errs)
	for err := range errs {
		t.Errorf("concurrent create: %v", err)
	}
	seen := map[string]bool{}
	for num := range numbers {
		if seen[num] {
			t.Errorf("number %s handed out twice", num)
		}
		seen[num] = true
	}
	if len(seen) != n {
		t.Errorf("got %d distinct numbers, want %d", len(seen), n)
	}
}
