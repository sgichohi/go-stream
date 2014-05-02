package mapper

import "github.com/cevian/go-stream/stream"

type Generator interface {
	GetWorker() Worker
}

func NewGenerator(mapCallback func(obj stream.Object, out Outputer), tn string) *SimpleGenerator {

	reseter := func(obj stream.FTResetter, out Outputer) {

		out.Out(1) <- obj
	}

	return &SimpleGenerator{MapCallback: mapCallback, ResetCallback: reseter, typename: tn}
}

type SimpleGenerator struct {
	MapCallback        func(obj stream.Object, out Outputer)
	CloseCallback      func(out Outputer)
	StopCallback       func()
	WorkerExitCallback func() //called once per worker
	SingleExitCallback func() //called once per op
	ResetCallback      func(obj stream.FTResetter, out Outputer)
	typename           string
}

func (g *SimpleGenerator) GetWorker() Worker {
	w := NewWorker(g.MapCallback, g.typename)
	w.CloseCallback = g.CloseCallback
	w.StopCallback = g.StopCallback
	w.ExitCallback = g.WorkerExitCallback
	w.ResetCallback = g.ResetCallback
	return w
}

func (w *SimpleGenerator) Exit() {
	if w.SingleExitCallback != nil {
		w.SingleExitCallback()
	}
}

type ClosureGenerator struct {
	createWorker       func() Worker
	singleExitCallback func()
	typename           string
}

func (w *ClosureGenerator) GetWorker() Worker {
	return w.createWorker()
}

func (w *ClosureGenerator) Exit() {
	if w.singleExitCallback != nil {
		w.singleExitCallback()
	}
}
