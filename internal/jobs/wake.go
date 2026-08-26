package jobs

type Wake struct{ ch chan struct{} }

func NewWake() *Wake { return &Wake{ch: make(chan struct{}, 1)} }
func (w *Wake) Signal() {
	select {
	case w.ch <- struct{}{}:
	default:
	}
}
func (w *Wake) C() <-chan struct{} { return w.ch }
