package syncutil

import (
	"context"
	"fmt"
	"sync"
)

type WaitGroupFunc func(context.Context) error

type WaitGroup struct {
	funcs []WaitGroupFunc

	wg *sync.WaitGroup
}

func (w *WaitGroup) Add(f WaitGroupFunc) {
	w.funcs = append(w.funcs, f)
}

func (w *WaitGroup) Wait(ctx context.Context) error {
	var (
		doneChan = make(chan bool)
		errChan  = make(chan error)
	)

	w.wg.Add(len(w.funcs))

	for _, v := range w.funcs {
		go func(f WaitGroupFunc) {
			if err := f(ctx); err != nil {
				errChan <- err
			}

			w.wg.Done()
		}(v)
	}

	go func() {
		w.wg.Wait()
		doneChan <- true
	}()

	select {
	case err := <-ctx.Done():
		return fmt.Errorf("%w: %s", context.Canceled, err)
	case <-doneChan:
		return nil
	case err := <-errChan:
		return err
	}

}

func NewWaitGroup() *WaitGroup {
	return &WaitGroup{
		funcs: []WaitGroupFunc{},
		wg:    &sync.WaitGroup{},
	}
}
