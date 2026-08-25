package staging

import "golang.org/x/sys/unix"

type Space struct {
	AvailableBytes int64
	TotalBytes     int64
}

type SpaceProbe interface{ Probe() (Space, error) }

type StatFSProbe struct{ root string }

func NewStatFSProbe(root string) *StatFSProbe { return &StatFSProbe{root: root} }

func (p *StatFSProbe) Probe() (Space, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(p.root, &stat); err != nil {
		return Space{}, ErrUnavailable
	}
	return Space{AvailableBytes: int64(stat.Bavail) * int64(stat.Bsize), TotalBytes: int64(stat.Blocks) * int64(stat.Bsize)}, nil
}
