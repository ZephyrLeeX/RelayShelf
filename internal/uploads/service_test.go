package uploads

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/staging"
	"github.com/google/uuid"
)

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time { return c.now }

type testIDs struct{ id uuid.UUID }

func (g testIDs) New() (uuid.UUID, error) { return g.id, nil }

type testSpace struct {
	value staging.Space
	err   error
}

func (p testSpace) Probe() (staging.Space, error) { return p.value, p.err }

type memoryRepo struct {
	mu                     sync.Mutex
	sessions               map[uuid.UUID]Session
	parts                  map[uuid.UUID]map[int]Part
	settings               Settings
	activeBytes, remaining int64
	events                 *[]string
	commitErr              error
	deletePartsErr         map[uuid.UUID]error
	markExpiredErr         map[uuid.UUID]error
	completeEntered        chan struct{}
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{sessions: map[uuid.UUID]Session{}, parts: map[uuid.UUID]map[int]Part{}, settings: Settings{MaxFileSizeBytes: 1 << 30, UploadRetentionHours: 24}, deletePartsErr: map[uuid.UUID]error{}, markExpiredErr: map[uuid.UUID]error{}}
}

type memoryInserter struct{ repo *memoryRepo }

func (i memoryInserter) Insert(_ context.Context, s Session) error {
	i.repo.sessions[s.ID] = s
	i.repo.parts[s.ID] = map[int]Part{}
	return nil
}
func (r *memoryRepo) WithCreateReservation(ctx context.Context, fn func(context.Context, Reservation, CreateInserter) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fn(ctx, Reservation{Settings: r.settings, ActiveBytes: r.activeBytes, ActiveRemaining: r.remaining}, memoryInserter{r})
}
func (r *memoryRepo) Get(_ context.Context, owner, id uuid.UUID) (Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok || s.UserID != owner {
		return Session{}, ErrNotFound
	}
	return s, nil
}
func (r *memoryRepo) ListParts(_ context.Context, id uuid.UUID) ([]Part, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Part{}
	for n := 0; n < len(r.parts[id]); n++ {
		if p, ok := r.parts[id][n]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}
func (r *memoryRepo) InvalidatePart(_ context.Context, id uuid.UUID, n int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.parts[id], n)
	if r.events != nil {
		*r.events = append(*r.events, "invalidate")
	}
	return nil
}
func (r *memoryRepo) CommitPart(_ context.Context, owner, id uuid.UUID, n int, size int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.commitErr != nil {
		return r.commitErr
	}
	s := r.sessions[id]
	if s.UserID != owner {
		return ErrNotFound
	}
	if !s.ExpiresAt.After(now) {
		return ErrExpired
	}
	r.parts[id][n] = Part{Number: n, SizeBytes: size, CompletedAt: now}
	s.Status = Uploading
	s.UpdatedAt = now
	r.sessions[id] = s
	if r.events != nil {
		*r.events = append(*r.events, "db")
	}
	return nil
}
func (r *memoryRepo) Complete(_ context.Context, owner, id uuid.UUID, now time.Time, validate func(Session, []Part) error) (Session, error) {
	if r.completeEntered != nil {
		select {
		case r.completeEntered <- struct{}{}:
		default:
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok || s.UserID != owner {
		return Session{}, ErrNotFound
	}
	if s.Status == Completing || s.Status == Completed {
		return s, nil
	}
	parts := []Part{}
	for n := 0; n < len(r.parts[id]); n++ {
		if p, ok := r.parts[id][n]; ok {
			parts = append(parts, p)
		}
	}
	if err := validate(s, parts); err != nil {
		return Session{}, err
	}
	s.Status = Completing
	s.UpdatedAt = now
	r.sessions[id] = s
	return s, nil
}
func (r *memoryRepo) FindDueActiveUploads(_ context.Context, now time.Time, batch int32) ([]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []uuid.UUID{}
	for id, s := range r.sessions {
		if int32(len(out)) >= batch {
			break
		}
		if (s.Status == Created || s.Status == Uploading || s.Status == Failed) && !s.ExpiresAt.After(now) {
			out = append(out, id)
		}
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return out, nil
}
func (r *memoryRepo) FindExpiredCleanupCandidates(_ context.Context) ([]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []uuid.UUID{}
	for id, s := range r.sessions {
		if s.Status == Expired && len(r.parts[id]) > 0 {
			out = append(out, id)
		}
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return out, nil
}
func (r *memoryRepo) MarkExpired(_ context.Context, id uuid.UUID, now time.Time) (Session, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.markExpiredErr[id]; err != nil {
		return Session{}, false, err
	}
	s, ok := r.sessions[id]
	if !ok {
		return Session{}, false, nil
	}
	if s.Status == Expired {
		return s, true, nil
	}
	if (s.Status == Created || s.Status == Uploading || s.Status == Failed) && !s.ExpiresAt.After(now) {
		s.Status = Expired
		r.sessions[id] = s
		return s, true, nil
	}
	return s, false, nil
}
func (r *memoryRepo) DeleteParts(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.deletePartsErr[id]; err != nil {
		return err
	}
	r.parts[id] = map[int]Part{}
	return nil
}
func (r *memoryRepo) ActiveUploadIDs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	active := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		s, ok := r.sessions[id]
		if ok && (s.Status == Created || s.Status == Uploading || s.Status == Completing || s.Status == Failed) {
			active[id] = struct{}{}
		}
	}
	return active, nil
}

type memoryStaging struct {
	mu                sync.Mutex
	files             map[uuid.UUID][]byte
	events            *[]string
	active, maxActive int32
	entered           chan struct{}
	release           <-chan struct{}
	writeErr          map[uuid.UUID]error
	deleteErr         map[uuid.UUID]error
	deleteAttempts    map[uuid.UUID]int
}

func newMemoryStaging() *memoryStaging {
	return &memoryStaging{files: map[uuid.UUID][]byte{}, writeErr: map[uuid.UUID]error{}, deleteErr: map[uuid.UUID]error{}, deleteAttempts: map[uuid.UUID]int{}}
}
func (m *memoryStaging) Create(id uuid.UUID, size int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[id] = make([]byte, size)
	return nil
}
func (m *memoryStaging) Open(id uuid.UUID) (staging.File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[id]; !ok {
		return nil, staging.ErrUnavailable
	}
	return &memoryFile{owner: m, id: id}, nil
}
func (m *memoryStaging) Stat(id uuid.UUID) (fs.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[id]
	if !ok {
		return nil, staging.ErrUnavailable
	}
	return memoryInfo{size: int64(len(b))}, nil
}
func (m *memoryStaging) Sync(id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[id]; !ok {
		return staging.ErrUnavailable
	}
	if m.events != nil {
		*m.events = append(*m.events, "sync")
	}
	return nil
}
func (m *memoryStaging) Delete(id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteAttempts[id]++
	if err := m.deleteErr[id]; err != nil {
		return err
	}
	delete(m.files, id)
	return nil
}
func (m *memoryStaging) Exists(id uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.files[id]
	return ok, nil
}
func (m *memoryStaging) OwnedFiles() ([]uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []uuid.UUID{}
	for id := range m.files {
		out = append(out, id)
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return out, nil
}

type memoryFile struct {
	owner *memoryStaging
	id    uuid.UUID
}

func (f *memoryFile) WriteAt(p []byte, off int64) (int, error) {
	active := atomic.AddInt32(&f.owner.active, 1)
	for {
		old := atomic.LoadInt32(&f.owner.maxActive)
		if active <= old || atomic.CompareAndSwapInt32(&f.owner.maxActive, old, active) {
			break
		}
	}
	defer atomic.AddInt32(&f.owner.active, -1)
	if f.owner.entered != nil {
		f.owner.entered <- struct{}{}
	}
	if f.owner.release != nil {
		<-f.owner.release
	}
	f.owner.mu.Lock()
	defer f.owner.mu.Unlock()
	if err := f.owner.writeErr[f.id]; err != nil {
		return 0, err
	}
	return copy(f.owner.files[f.id][off:], p), nil
}
func (f *memoryFile) Sync() error {
	f.owner.mu.Lock()
	defer f.owner.mu.Unlock()
	if f.owner.events != nil {
		*f.owner.events = append(*f.owner.events, "sync")
	}
	return nil
}
func (f *memoryFile) Stat() (fs.FileInfo, error) { return f.owner.Stat(f.id) }
func (f *memoryFile) Close() error               { return nil }

type memoryInfo struct{ size int64 }

func (memoryInfo) Name() string       { return "upload" }
func (i memoryInfo) Size() int64      { return i.size }
func (memoryInfo) Mode() fs.FileMode  { return 0o600 }
func (memoryInfo) ModTime() time.Time { return time.Time{} }
func (memoryInfo) IsDir() bool        { return false }
func (memoryInfo) Sys() any           { return nil }

func testService(repo *memoryRepo, stage *memoryStaging, c *testClock, maxWrites int, id uuid.UUID) *Service {
	return NewService(repo, stage, testSpace{value: staging.Space{AvailableBytes: 1 << 40, TotalBytes: 2 << 40}}, testIDs{id}, c, NewLockRegistry(), maxWrites, 1<<40, 0, 0)
}
func putSession(repo *memoryRepo, stage *memoryStaging, id, owner uuid.UUID, size int64, c *testClock) {
	repo.sessions[id] = Session{ID: id, UserID: owner, ExpectedSize: size, ChunkSize: ChunkSize, Status: Created, ExpiresAt: c.now.Add(time.Hour), CreatedAt: c.now, UpdatedAt: c.now}
	repo.parts[id] = map[int]Part{}
	_ = stage.Create(id, size)
}

type brokenReader struct{ sent bool }

func (r *brokenReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		copy(p, "ab")
		return 2, nil
	}
	return 0, errors.New("interrupted")
}

func TestCreateValidatesMetadataQuotaAndProjectedSpace(t *testing.T) {
	c := &testClock{time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)}
	owner := uuid.New()
	id := uuid.New()
	repo := newMemoryRepo()
	stage := newMemoryStaging()
	service := testService(repo, stage, c, 8, id)
	row, err := service.Create(context.Background(), owner, CreateCommand{OriginalFilename: "archive.zip", ExpectedSize: 0})
	if err != nil || row.ChunkSize != ChunkSize || row.PartCount() != 0 || !row.ExpiresAt.Equal(c.now.Add(24*time.Hour)) {
		t.Fatalf("row=%+v err=%v", row, err)
	}
	if _, err = service.Create(context.Background(), owner, CreateCommand{OriginalFilename: "../../etc/passwd", ExpectedSize: 0}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unsafe filename=%v", err)
	}
	repo.activeBytes = 1 << 40
	if _, err = service.Create(context.Background(), owner, CreateCommand{OriginalFilename: "x", ExpectedSize: 1}); !errors.Is(err, ErrStagingFull) {
		t.Fatalf("quota=%v", err)
	}
	repo.activeBytes = 0
	service.space = testSpace{value: staging.Space{AvailableBytes: 100, TotalBytes: 1000}}
	service.minFreeBytes = 20
	repo.remaining = 90
	if _, err = service.Create(context.Background(), owner, CreateCommand{OriginalFilename: "x", ExpectedSize: 1}); !errors.Is(err, ErrStagingFull) {
		t.Fatalf("projected=%v", err)
	}
	repo2, stage2 := newMemoryRepo(), newMemoryStaging()
	percentService := testService(repo2, stage2, c, 8, uuid.New())
	percentService.space = testSpace{value: staging.Space{AvailableBytes: 99, TotalBytes: 1000}}
	percentService.minFreePercent = 10
	if _, err = percentService.Create(context.Background(), owner, CreateCommand{OriginalFilename: "percent", ExpectedSize: 0}); !errors.Is(err, ErrStagingFull) {
		t.Fatalf("percent guard=%v", err)
	}
}

func TestInterruptedRetryInvalidatesMarkerAndSyncPrecedesDB(t *testing.T) {
	c := &testClock{time.Now().UTC()}
	owner, id := uuid.New(), uuid.New()
	repo := newMemoryRepo()
	stage := newMemoryStaging()
	service := testService(repo, stage, c, 8, id)
	putSession(repo, stage, id, owner, 4, c)
	repo.parts[id][0] = Part{Number: 0, SizeBytes: 4}
	err := service.PutPart(context.Background(), owner, id, 0, -1, &brokenReader{})
	if err == nil {
		t.Fatal("interruption succeeded")
	}
	if errors.Is(err, ErrStagingUnavailable) {
		t.Fatalf("reader error was mapped as staging failure: %v", err)
	}
	if len(repo.parts[id]) != 0 {
		t.Fatal("old marker survived failed retry")
	}
	if _, err = service.Complete(context.Background(), owner, id); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("complete trusted partial bytes: %v", err)
	}
	events := []string{}
	repo.events = &events
	stage.events = &events
	if err = service.PutPart(context.Background(), owner, id, 0, 4, bytes.NewReader([]byte("abcd"))); err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 || events[0] != "invalidate" || events[1] != "sync" || events[2] != "db" {
		t.Fatalf("ordering=%v", events)
	}
	repo.commitErr = errors.New("injected commit failure")
	if err = service.PutPart(context.Background(), owner, id, 0, 4, bytes.NewReader([]byte("fail"))); err == nil {
		t.Fatal("injected commit failure succeeded")
	}
	if len(repo.parts[id]) != 0 {
		t.Fatal("marker survived DB failure")
	}
	repo.commitErr = nil
	if err = service.PutPart(context.Background(), owner, id, 0, 4, bytes.NewReader([]byte("safe"))); err != nil {
		t.Fatal(err)
	}
}

func TestLongBodyDoesNotWritePastPartRange(t *testing.T) {
	c := &testClock{time.Now().UTC()}
	owner, id := uuid.New(), uuid.New()
	repo := newMemoryRepo()
	stage := newMemoryStaging()
	service := testService(repo, stage, c, 8, id)
	putSession(repo, stage, id, owner, ChunkSize+2, c)
	body := bytes.Repeat([]byte{'a'}, int(ChunkSize+1))
	if err := service.PutPart(context.Background(), owner, id, 0, -1, bytes.NewReader(body)); !errors.Is(err, ErrPartSizeMismatch) {
		t.Fatalf("err=%v", err)
	}
	stage.mu.Lock()
	defer stage.mu.Unlock()
	if stage.files[id][ChunkSize] != 0 {
		t.Fatal("extra byte polluted adjacent part")
	}
	if len(repo.parts[id]) != 0 {
		t.Fatal("oversized part received marker")
	}
}

func TestCompleteWaitsForChunkAndSeesDurableMarker(t *testing.T) {
	c := &testClock{time.Now().UTC()}
	owner, id := uuid.New(), uuid.New()
	repo := newMemoryRepo()
	repo.completeEntered = make(chan struct{}, 1)
	stage := newMemoryStaging()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	stage.entered = entered
	stage.release = release
	service := testService(repo, stage, c, 8, id)
	putSession(repo, stage, id, owner, 4, c)
	chunkDone := make(chan error, 1)
	go func() {
		chunkDone <- service.PutPart(context.Background(), owner, id, 0, 4, bytes.NewReader([]byte("data")))
	}()
	<-entered
	completeDone := make(chan error, 1)
	go func() { _, err := service.Complete(context.Background(), owner, id); completeDone <- err }()
	select {
	case <-repo.completeEntered:
		t.Fatal("complete entered DB before chunk finished")
	default:
	}
	close(release)
	if err := <-chunkDone; err != nil {
		t.Fatal(err)
	}
	if err := <-completeDone; err != nil {
		t.Fatal(err)
	}
	if repo.sessions[id].Status != Completing {
		t.Fatalf("status=%s", repo.sessions[id].Status)
	}
}

func TestGlobalWriteSemaphoreAndExpirationCleanup(t *testing.T) {
	c := &testClock{time.Now().UTC()}
	owner := uuid.New()
	repo := newMemoryRepo()
	stage := newMemoryStaging()
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	stage.entered = entered
	stage.release = release
	service := testService(repo, stage, c, 2, uuid.New())
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for _, id := range ids {
		putSession(repo, stage, id, owner, 1, c)
	}
	done := make(chan error, 3)
	for _, id := range ids {
		go func(id uuid.UUID) {
			done <- service.PutPart(context.Background(), owner, id, 0, 1, bytes.NewReader([]byte("x")))
		}(id)
	}
	<-entered
	<-entered
	select {
	case <-entered:
		t.Fatal("more than two writers entered filesystem")
	default:
	}
	close(release)
	for range ids {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if stage.maxActive > 2 {
		t.Fatalf("max active=%d", stage.maxActive)
	}
	expiredID := uuid.New()
	putSession(repo, stage, expiredID, owner, 1, c)
	s := repo.sessions[expiredID]
	s.ExpiresAt = c.now
	repo.sessions[expiredID] = s
	repo.parts[expiredID][0] = Part{Number: 0, SizeBytes: 1}
	if err := service.ExpireDueUploads(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	exists, _ := stage.Exists(expiredID)
	if exists || len(repo.parts[expiredID]) != 0 || repo.sessions[expiredID].Status != Expired {
		t.Fatal("expiration cleanup incomplete")
	}
}

func TestCompleteZeroByteAndRejectsCorruptStaging(t *testing.T) {
	c := &testClock{time.Now().UTC()}
	owner := uuid.New()
	repo, stage := newMemoryRepo(), newMemoryStaging()
	zeroID := uuid.New()
	service := testService(repo, stage, c, 8, zeroID)
	putSession(repo, stage, zeroID, owner, 0, c)
	row, err := service.Complete(context.Background(), owner, zeroID)
	if err != nil || row.Status != Completing {
		t.Fatalf("zero complete=%+v err=%v", row, err)
	}
	corruptID := uuid.New()
	putSession(repo, stage, corruptID, owner, 1, c)
	stage.mu.Lock()
	stage.files[corruptID] = []byte{0, 0}
	stage.mu.Unlock()
	if _, err = service.Complete(context.Background(), owner, corruptID); !errors.Is(err, ErrStagingCorrupt) {
		t.Fatalf("corrupt staging=%v", err)
	}
}

func TestReconcileStagingFindsOrphanAfterActivePrefix(t *testing.T) {
	c := &testClock{time.Now().UTC()}
	owner := uuid.New()
	repo := newMemoryRepo()
	root := t.TempDir()
	stage, err := staging.New(root)
	if err != nil {
		t.Fatal(err)
	}
	activeIDs := []uuid.UUID{
		uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		uuid.MustParse("00000000-0000-4000-8000-000000000002"),
		uuid.MustParse("00000000-0000-4000-8000-000000000003"),
	}
	for _, id := range activeIDs {
		repo.sessions[id] = Session{ID: id, UserID: owner, ExpectedSize: 0, ChunkSize: ChunkSize, Status: Created, ExpiresAt: c.now.Add(time.Hour)}
		repo.parts[id] = map[int]Part{}
		if err = stage.Create(id, 0); err != nil {
			t.Fatal(err)
		}
	}
	orphan := uuid.MustParse("ffffffff-ffff-4fff-bfff-ffffffffffff")
	if err = stage.Create(orphan, 0); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(root, "keep.me")
	if err = os.WriteFile(unknown, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService(repo, stage, testSpace{value: staging.Space{AvailableBytes: 1 << 40, TotalBytes: 2 << 40}}, testIDs{uuid.New()}, c, NewLockRegistry(), 8, 1<<40, 0, 0)
	if err = service.ReconcileStaging(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if exists, existsErr := stage.Exists(orphan); existsErr != nil || exists {
		t.Fatalf("orphan exists=%v err=%v", exists, existsErr)
	}
	for _, id := range activeIDs {
		if exists, existsErr := stage.Exists(id); existsErr != nil || !exists {
			t.Fatalf("active %s exists=%v err=%v", id, exists, existsErr)
		}
	}
	if data, readErr := os.ReadFile(unknown); readErr != nil || string(data) != "safe" {
		t.Fatalf("unknown staging file changed: data=%q err=%v", data, readErr)
	}
}

func TestExpirationCleanupFailureDoesNotBlockBatch(t *testing.T) {
	c := &testClock{time.Now().UTC()}
	owner := uuid.New()
	repo, stage := newMemoryRepo(), newMemoryStaging()
	service := testService(repo, stage, c, 8, uuid.New())
	ids := []uuid.UUID{
		uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		uuid.MustParse("00000000-0000-4000-8000-000000000002"),
		uuid.MustParse("00000000-0000-4000-8000-000000000003"),
	}
	for _, id := range ids {
		putSession(repo, stage, id, owner, 1, c)
		s := repo.sessions[id]
		s.ExpiresAt = c.now
		repo.sessions[id] = s
		repo.parts[id][0] = Part{Number: 0, SizeBytes: 1}
	}
	broken := ids[0]
	stage.deleteErr[broken] = syscall.EIO
	if err := service.ExpireDueUploads(context.Background(), 3); err == nil {
		t.Fatal("permanent cleanup failure was not reported")
	}
	for _, id := range ids {
		if repo.sessions[id].Status != Expired {
			t.Fatalf("upload %s status=%s", id, repo.sessions[id].Status)
		}
	}
	for _, id := range ids[1:] {
		if exists, _ := stage.Exists(id); exists || len(repo.parts[id]) != 0 {
			t.Fatalf("healthy upload %s was not cleaned", id)
		}
	}
	if exists, _ := stage.Exists(broken); !exists || len(repo.parts[broken]) == 0 {
		t.Fatal("failed upload lost retryable cleanup state")
	}
	before := stage.deleteAttempts[broken]
	if err := service.ExpireDueUploads(context.Background(), 3); err == nil {
		t.Fatal("retry failure was not reported")
	}
	if stage.deleteAttempts[broken] <= before {
		t.Fatal("next cleanup did not retry failed upload")
	}
}

func TestOrphanDeleteFailureDoesNotBlockDueExpiration(t *testing.T) {
	c := &testClock{time.Now().UTC()}
	owner := uuid.New()
	repo, stage := newMemoryRepo(), newMemoryStaging()
	service := testService(repo, stage, c, 8, uuid.New())
	orphan := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	due := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	_ = stage.Create(orphan, 0)
	stage.deleteErr[orphan] = syscall.EIO
	putSession(repo, stage, due, owner, 0, c)
	s := repo.sessions[due]
	s.ExpiresAt = c.now
	repo.sessions[due] = s
	if err := service.ExpireDueUploads(context.Background(), 2); err == nil {
		t.Fatal("orphan cleanup failure was not reported")
	}
	if repo.sessions[due].Status != Expired {
		t.Fatalf("due upload status=%s", repo.sessions[due].Status)
	}
	if exists, _ := stage.Exists(due); exists {
		t.Fatal("due upload staging was not cleaned")
	}
}

func TestPartsDeleteFailureDoesNotBlockExpiredCleanup(t *testing.T) {
	c := &testClock{time.Now().UTC()}
	owner := uuid.New()
	repo, stage := newMemoryRepo(), newMemoryStaging()
	service := testService(repo, stage, c, 8, uuid.New())
	broken := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	healthy := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	for _, id := range []uuid.UUID{broken, healthy} {
		putSession(repo, stage, id, owner, 1, c)
		s := repo.sessions[id]
		s.Status = Expired
		repo.sessions[id] = s
		repo.parts[id][0] = Part{Number: 0, SizeBytes: 1}
	}
	repo.deletePartsErr[broken] = errors.New("database unavailable")
	if err := service.ExpireDueUploads(context.Background(), 2); err == nil {
		t.Fatal("parts cleanup failure was not reported")
	}
	if len(repo.parts[broken]) == 0 {
		t.Fatal("failed parts cleanup lost retry state")
	}
	if exists, _ := stage.Exists(healthy); exists || len(repo.parts[healthy]) != 0 {
		t.Fatal("healthy expired upload was blocked")
	}
	delete(repo.deletePartsErr, broken)
	if err := service.ExpireDueUploads(context.Background(), 2); err != nil {
		t.Fatalf("parts cleanup retry: %v", err)
	}
	if len(repo.parts[broken]) != 0 {
		t.Fatal("parts cleanup retry did not complete")
	}
}

func TestWriteAtFailureMapsToStagingUnavailableAndCanRetry(t *testing.T) {
	for _, writeErr := range []error{syscall.ENOSPC, syscall.EIO} {
		t.Run(writeErr.Error(), func(t *testing.T) {
			c := &testClock{time.Now().UTC()}
			owner, id := uuid.New(), uuid.New()
			repo, stage := newMemoryRepo(), newMemoryStaging()
			service := testService(repo, stage, c, 8, id)
			putSession(repo, stage, id, owner, 4, c)
			repo.parts[id][0] = Part{Number: 0, SizeBytes: 4}
			stage.writeErr[id] = writeErr
			err := service.PutPart(context.Background(), owner, id, 0, 4, bytes.NewReader([]byte("data")))
			if !errors.Is(err, ErrStagingUnavailable) {
				t.Fatalf("err=%v", err)
			}
			if len(repo.parts[id]) != 0 {
				t.Fatal("WriteAt failure created a part marker")
			}
			delete(stage.writeErr, id)
			if err = service.PutPart(context.Background(), owner, id, 0, 4, bytes.NewReader([]byte("data"))); err != nil {
				t.Fatalf("healthy retry: %v", err)
			}
			if len(repo.parts[id]) != 1 {
				t.Fatal("healthy retry did not create marker")
			}
		})
	}
}

var _ io.Reader = (*brokenReader)(nil)
