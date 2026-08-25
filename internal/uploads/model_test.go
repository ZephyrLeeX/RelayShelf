package uploads

import "testing"

func TestPartCountAndSizes(t *testing.T) {
	for _, test := range []struct {
		size  int64
		count int
		sizes []int64
	}{
		{0, 0, nil}, {1, 1, []int64{1}}, {ChunkSize, 1, []int64{ChunkSize}}, {ChunkSize + 3, 2, []int64{ChunkSize, 3}}, {2 * ChunkSize, 2, []int64{ChunkSize, ChunkSize}},
	} {
		s := Session{ExpectedSize: test.size, ChunkSize: ChunkSize}
		if s.PartCount() != test.count {
			t.Fatalf("size=%d count=%d", test.size, s.PartCount())
		}
		for n, want := range test.sizes {
			if got := s.PartSize(n); got != want {
				t.Fatalf("size=%d part=%d got=%d want=%d", test.size, n, got, want)
			}
		}
		if s.PartSize(-1) != -1 || s.PartSize(test.count) != -1 {
			t.Fatalf("out of range accepted for %d", test.size)
		}
	}
}
