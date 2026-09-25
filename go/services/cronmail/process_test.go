package cronmail

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// fakeStore — in-memory Store implementing the node claim protocol to
// exercise Run()'s loop semantics without a live Mongo.
type fakeStore struct {
	docs       []*Doc
	deleted    []bson.Raw
	markRetry  []string
	markDead   []string
	released   []bson.Raw
	users      map[string]string
	claimCalls int
	now        func() time.Time
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[string]string{}, now: time.Now}
}

func (f *fakeStore) claimable(d *Doc, now time.Time) bool {
	if d.Dead {
		return false
	}
	if d.ScheduledAt.After(now) {
		return false
	}
	switch {
	case !d.ProcessingSet:
		return true
	case d.Processing == false && (d.NextRetryAt == nil || d.NextRetryAt.Before(now)):
		return true
	case d.Processing == true && d.ProcessingStartedAt != nil &&
		d.ProcessingStartedAt.Before(now.Add(-staleProcessing)):
		return true
	}
	return false
}

func (f *fakeStore) ClaimNextDue(ctx context.Context, now time.Time) (*Doc, error) {
	f.claimCalls++
	var best *Doc
	for _, d := range f.docs {
		if !f.claimable(d, now) {
			continue
		}
		if best == nil || d.ScheduledAt.Before(best.ScheduledAt) {
			best = d
		}
	}
	if best != nil {
		best.Processing = true
		best.ProcessingSet = true
		t := now
		best.ProcessingStartedAt = &t
	}
	if best == nil {
		return nil, nil
	}
	c := *best
	return &c, nil
}

func (f *fakeStore) byID(id bson.Raw) *Doc {
	for _, d := range f.docs {
		if string(d.ID) == string(id) {
			return d
		}
	}
	panic("doc not found")
}

func (f *fakeStore) MarkRetry(ctx context.Context, id bson.Raw, attempts int, nextRetryAt time.Time, errMsg string) error {
	d := f.byID(id)
	d.Processing = false
	d.ProcessingSet = true
	d.NextRetryAt = &nextRetryAt
	d.Attempts = attempts
	d.Error = errMsg
	f.markRetry = append(f.markRetry, strconv.Itoa(attempts)+"|"+errMsg)
	return nil
}

func (f *fakeStore) MarkDead(ctx context.Context, id bson.Raw, attempts int, errMsg string) error {
	d := f.byID(id)
	d.Processing = false
	d.ProcessingSet = true
	d.Attempts = attempts
	d.Dead = true
	d.Error = errMsg
	f.markDead = append(f.markDead, strconv.Itoa(attempts)+"|"+errMsg)
	return nil
}

func (f *fakeStore) DeleteDoc(ctx context.Context, id bson.Raw) error {
	f.deleted = append(f.deleted, id)
	for i, d := range f.docs {
		if string(d.ID) == string(id) {
			f.docs = append(f.docs[:i], f.docs[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeStore) ReleaseMany(ctx context.Context, ids []bson.Raw) error {
	f.released = append(f.released, ids...)
	for _, id := range ids {
		d := f.byID(id)
		d.Processing = false
		d.ProcessingSet = false
		d.NextRetryAt = nil
		d.ProcessingStartedAt = nil
	}
	return nil
}

func (f *fakeStore) GetUserEmail(ctx context.Context, id any) (string, error) {
	if s, ok := id.(string); ok {
		return f.users[s], nil
	}
	return "", nil
}

type captureMail struct {
	sent   []string
	failOn func(to, subject string) bool
}

func (c *captureMail) Send(to, subject, text, html string) error {
	if c.failOn != nil && c.failOn(to, subject) {
		return context.DeadlineExceeded
	}
	c.sent = append(c.sent, to+"|"+subject)
	return nil
}

var testNow = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

func mkDoc(id, emailType, legacyType string, raw map[string]any) *Doc {
	b, _ := bson.Marshal(id)
	return &Doc{ID: b, EmailType: emailType, Type: legacyType, Raw: raw, ScheduledAt: testNow.Add(-time.Minute)}
}

var testSettings = Settings{AppName: "OlliTeX", SiteURL: "http://127.0.0.1:4000", Env: "server-ce"}
var testCfg = Config{BatchSize: 100, MaxAttempts: 3, BackoffBase: 2 * time.Hour, DryRun: false}

func TestRunHappyPath(t *testing.T) {
	st := newFakeStore()
	st.docs = []*Doc{
		mkDoc("id1", "projectNotification", "", map[string]any{
			"projectId": "p1", "projectName": "Proj One", "isComment": true, "toUserId": "u1"}),
		mkDoc("id2", "trackedChangesNotification", "", map[string]any{
			"projectId": "p2", "projectName": "Proj Two", "toUserId": "u2"}),
	}
	st.users = map[string]string{"u1": "one@x.test", "u2": "two@x.test"}

	mail := &captureMail{}
	stats, err := Run(context.Background(), st, mail, testSettings, testCfg)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stats.NotificationsFound != 2 || stats.NotificationsReady != 2 || stats.EmailsSent != 2 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(mail.sent) != 2 || len(st.deleted) != 2 {
		t.Fatalf("sent=%d deleted=%d", len(mail.sent), len(st.deleted))
	}
	if len(st.markRetry) != 0 || len(st.markDead) != 0 {
		t.Fatalf("unexpected failures retry=%v dead=%v", st.markRetry, st.markDead)
	}
	if !strings.HasPrefix(mail.sent[0], "one@x.test|") || !strings.HasPrefix(mail.sent[1], "two@x.test|") {
		t.Fatalf("to-addresses: %#v", mail.sent)
	}
	// no re-claim possible: queue drained
	if got, _ := st.ClaimNextDue(context.Background(), testNow); got != nil {
		t.Fatalf("queue should be drained, got %+v", got)
	}
}

func TestRunDryRun(t *testing.T) {
	st := newFakeStore()
	st.docs = []*Doc{mkDoc("d1", "projectNotification", "", map[string]any{
		"projectId": "p9", "projectName": "Dry", "isComment": false, "to": "dry@x.test"})}

	cfg := testCfg
	cfg.DryRun = true
	mail := &captureMail{}
	stats, err := Run(context.Background(), st, mail, testSettings, cfg)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(mail.sent) != 0 {
		t.Fatalf("dry-run sent %d mails", len(mail.sent))
	}
	if stats.DryRunProcessed != 1 || stats.DryRunWouldHaveSent != 1 {
		t.Fatalf("dry stats = %+v", stats)
	}
	if len(st.released) != 1 {
		t.Fatalf("released %v, want 1", st.released)
	}
	d := st.byID(bsonMust("d1"))
	if d.Processing || d.ProcessingSet {
		t.Fatalf("dry-run did not release claim: %+v", d)
	}
	// still claimable + present
	if got, _ := st.ClaimNextDue(context.Background(), testNow); got == nil {
		t.Fatalf("dry-run should leave doc claimable")
	}
}

func TestRunRetryThenDead(t *testing.T) {
	st := newFakeStore()
	d := mkDoc("f1", "projectNotification", "", map[string]any{
		"projectId": "p", "projectName": "Fail", "isComment": true, "to": "f@x.test"})
	st.docs = []*Doc{d}

	mail := &captureMail{failOn: func(to, subject string) bool { return true }}

	// attempt 1 → retry(1)
	if _, err := Run(context.Background(), st, mail, testSettings, testCfg); err != nil {
		t.Fatal(err)
	}
	if len(st.markRetry) != 1 {
		t.Fatalf("attempt1 retry=%v", st.markRetry)
	}
	if d.Attempts != 1 {
		t.Fatalf("attempts=%d want 1", d.Attempts)
	}

	// simulate the backoff window elapsing (node: nextRetryAt = now + 2^attempts*base)
	past := testNow.Add(-time.Second)
	d.NextRetryAt = &past

	// attempt 2 → retry(2)
	st.markRetry = nil
	if _, err := Run(context.Background(), st, mail, testSettings, testCfg); err != nil {
		t.Fatal(err)
	}
	if len(st.markRetry) != 1 {
		t.Fatalf("attempt2 retry=%v", st.markRetry)
	}
	if d.Attempts != 2 {
		t.Fatalf("attempts=%d want 2", d.Attempts)
	}
	d.NextRetryAt = &past // backoff elapsed again

	// attempt 3 → dead (MaxAttempts=3)
	st.markRetry = nil
	stats, err := Run(context.Background(), st, mail, testSettings, testCfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.markDead) != 1 {
		t.Fatalf("attempt3 dead=%v (retry=%v)", st.markDead, st.markRetry)
	}
	if d.Attempts != 3 || !d.Dead {
		t.Fatalf("attempts=%d dead=%v", d.Attempts, d.Dead)
	}
	if len(st.deleted) != 0 {
		t.Fatalf("deleted after failure: %v", st.deleted)
	}
	_ = stats
}

func TestRunMissingEmailTypeFails(t *testing.T) {
	st := newFakeStore()
	st.docs = []*Doc{mkDoc("m1", "", "", map[string]any{"to": "m@x.test"})}

	mail := &captureMail{}
	if _, err := Run(context.Background(), st, mail, testSettings, testCfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(mail.sent) != 0 {
		t.Fatalf("sent=%v", mail.sent)
	}
	if len(st.markRetry) != 1 {
		t.Fatalf("retry=%v want 1 (missing emailType)", st.markRetry)
	}
	if !strings.Contains(st.markRetry[0], "missing emailType") {
		t.Fatalf("retry msg: %q", st.markRetry[0])
	}
}

func TestRunLegacyTypeField(t *testing.T) {
	st := newFakeStore()
	st.docs = []*Doc{mkDoc("l1", "", "projectNotification", map[string]any{
		"to": "l@x.test", "projectId": "pl", "projectName": "Legacy", "isComment": true})}

	mail := &captureMail{}
	stats, err := Run(context.Background(), st, mail, testSettings, testCfg)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stats.EmailsSent != 1 {
		t.Fatalf("stats=%+v", stats)
	}
	if !strings.Contains(mail.sent[0], "New project comment") {
		t.Fatalf("subject: %q", mail.sent[0])
	}
}

func TestRunUnknownEmailTypeFails(t *testing.T) {
	st := newFakeStore()
	st.docs = []*Doc{mkDoc("u1", "notARealType", "", map[string]any{"to": "u@x.test"})}

	mail := &captureMail{}
	if _, err := Run(context.Background(), st, mail, testSettings, testCfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(mail.sent) != 0 {
		t.Fatalf("sent=%v (unknown type must not send)", mail.sent)
	}
	if len(st.markRetry)+len(st.markDead) != 1 {
		t.Fatalf("failures: retry=%v dead=%v", st.markRetry, st.markDead)
	}
}

func TestRunResolvesRecipientFromUserId(t *testing.T) {
	st := newFakeStore()
	d := mkDoc("r1", "projectNotification", "", map[string]any{
		"toUserId": "user42", "projectId": "pa", "projectName": "Recip", "isComment": true})
	st.docs = []*Doc{d}
	st.users = map[string]string{"user42": "resolved@x.test"}

	mail := &captureMail{}
	stats, err := Run(context.Background(), st, mail, testSettings, testCfg)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stats.EmailsSent != 1 {
		t.Fatalf("stats=%+v", stats)
	}
	if !strings.HasPrefix(mail.sent[0], "resolved@x.test|") {
		t.Fatalf("to: %q", mail.sent[0])
	}
	if _, has := d.Raw["toUserId"]; has {
		t.Fatalf("toUserId should be removed after resolution")
	}
}

func TestRunRecipientMissingFails(t *testing.T) {
	st := newFakeStore()
	st.docs = []*Doc{mkDoc("rm1", "projectNotification", "", map[string]any{
		"toUserId": "ghost", "projectId": "pb", "projectName": "Ghost", "isComment": false})}
	// users empty → no email → fail, do not send

	mail := &captureMail{}
	if _, err := Run(context.Background(), st, mail, testSettings, testCfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(mail.sent) != 0 {
		t.Fatalf("sent=%v (missing recipient must not send)", mail.sent)
	}
	if len(st.markRetry)+len(st.markDead) != 1 {
		t.Fatalf("failures: retry=%v dead=%v", st.markRetry, st.markDead)
	}
}

func TestRunBatchSizeCap(t *testing.T) {
	st := newFakeStore()
	for i := 0; i < 5; i++ {
		st.docs = append(st.docs, mkDoc("b"+strconv.Itoa(i), "projectNotification", "",
			map[string]any{"projectId": "p", "projectName": "B" + strconv.Itoa(i), "isComment": true,
				"to": "b@x.test"}))
	}
	cfg := testCfg
	cfg.BatchSize = 2
	mail := &captureMail{}
	stats, err := Run(context.Background(), st, mail, testSettings, cfg)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stats.EmailsSent != 2 {
		t.Fatalf("batch cap: sent=%d want 2", stats.EmailsSent)
	}
	// remaining 3 still claimable
	remaining := 0
	for {
		d, _ := st.ClaimNextDue(context.Background(), testNow)
		if d == nil {
			break
		}
		remaining++
	}
	if remaining != 3 {
		t.Fatalf("remaining claimable=%d want 3", remaining)
	}
}

func TestRunStaleReclaim(t *testing.T) {
	st := newFakeStore()
	d := mkDoc("s1", "projectNotification", "", map[string]any{
		"projectId": "p", "projectName": "Stale", "isComment": true, "to": "s@x.test"})
	// simulate a claim older than staleProcessing (consumer died)
	start := testNow.Add(-2 * time.Hour)
	d.Processing = true
	d.ProcessingSet = true
	d.ProcessingStartedAt = &start
	st.docs = []*Doc{d}

	mail := &captureMail{}
	stats, err := Run(context.Background(), st, mail, testSettings, testCfg)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stats.EmailsSent != 1 {
		t.Fatalf("stale reclaim: sent=%d want 1", stats.EmailsSent)
	}
	_ = start
}

func bsonMust(id string) bson.Raw {
	b, _ := bson.Marshal(id)
	return b
}
