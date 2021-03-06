package mapper

import "runtime"

import "sync"
import "github.com/cevian/go-stream/stream"

import "log"

func NewOrderedOp(mapCallback func(obj stream.Object, out Outputer) error, tn string) *OrderPreservingOp {
	gen := NewGenerator(mapCallback, tn)
	mop := NewOpFromGenerator(gen, tn)
	return NewOrderedOpWrapper(mop)
}

func NewOrderedOpWrapper(op *Op) *OrderPreservingOp {
	o := OrderPreservingOp{Op: op, ConcurrentErrorHandler: NewConcurrentErrorHandler()}
	o.Init()
	return &o
}

type OrderPreservingOutputer struct {
	sent         bool
	out          chan<- stream.Object
	num          chan<- int
	stopNotifier <-chan bool
}

func (o *OrderPreservingOutputer) Out(num int) chan<- stream.Object {
	o.sent = true
	o.num <- num
	return o.out
}

func (o *OrderPreservingOutputer) Sending(num int) Sender {
	o.sent = true
	o.num <- num
	return o
}

func (o *OrderPreservingOutputer) Send(rec stream.Object) {
	select {
	case o.out <- rec:
	case <-o.stopNotifier:
	}
}

func NewOrderPreservingOutputer(out chan<- stream.Object, num chan<- int, stopNotifier <-chan bool) *OrderPreservingOutputer {
	return &OrderPreservingOutputer{false, out, num, stopNotifier}
}

type OrderPreservingOp struct {
	*Op
	*ConcurrentErrorHandler
	results    []chan stream.Object //[]chan O
	resultsNum []chan int
	resultQ    chan int
	lock       chan bool
}

func (o *OrderPreservingOp) IsOrdered() bool {
	return true
}

func (o *OrderPreservingOp) MakeOrdered() stream.ParallelizableOperator {
	panic("Already Ordered")
}

func (o *OrderPreservingOp) runWorker(worker Worker, workerid int) {
	outputer := NewOrderPreservingOutputer(o.results[workerid], o.resultsNum[workerid], o.StopNotifier)
	for {
		<-o.lock
		select {
		case obj, ok := <-o.In():
			if ok {
				o.resultQ <- workerid
				o.lock <- true
				outputer.sent = false
				err := worker.Map(obj, outputer)
				if !outputer.sent {
					o.resultsNum[workerid] <- 0
				}
				if err != nil {
					o.SetError(err)
					o.Stop()
					return
				}
			} else {
				o.resultQ <- workerid
				o.lock <- true
				outputer.sent = false
				err := o.WorkerClose(worker, outputer)
				if !outputer.sent {
					o.resultsNum[workerid] <- 0
				}
				if err != nil {
					o.SetError(err)
					o.Stop()
				}
				return
			}
		case <-o.StopNotifier:
			o.WorkerStop(worker)
			o.lock <- true
			return
		}

	}
}

func (p *OrderPreservingOp) Combiner() {
	for workerid := range p.resultQ {
		num_entries := <-p.resultsNum[workerid]
		for l := 0; l < num_entries; l++ {
			select {
			case val, ok := <-p.results[workerid]:
				if !ok {
					log.Panic("Should never get a closed channel here")
				}
				select {
				case p.Out() <- val:
				case <-p.StopNotifier:
				}
			case <-p.StopNotifier:

			}
		}
	}
}

func (proc *OrderPreservingOp) InitiateWorkerChannels(numWorkers int) {
	//results holds the result for each worker so [workerid] chan RESULTTYPE
	proc.results = make([]chan stream.Object, numWorkers)
	//resultsNum holds the number of output tuples put into the results slice by a single input tuple
	//in one run. It is indexed by a workerid
	proc.resultsNum = make([]chan int, numWorkers)
	for i := 0; i < numWorkers; i++ {
		resultch := make(chan stream.Object, 1)
		proc.results[i] = resultch
		numch := make(chan int, 3)
		proc.resultsNum[i] = numch
	}
	proc.resultQ = make(chan int, numWorkers*2) //the ordering in which workers recieved input tuples

	proc.lock = make(chan bool, 1)
	proc.lock <- true

}

func (o *OrderPreservingOp) Run() error {
	defer close(o.Out())
	//perform some validation
	//Processor.Validate()

	maxWorkers := o.MaxWorkers
	if !o.Parallel {
		maxWorkers = 1
	} else if o.MaxWorkers == 0 {
		maxWorkers = runtime.NumCPU()
	}
	o.InitiateWorkerChannels(maxWorkers)

	println("Starting ", maxWorkers, " orderpreserving workers for ", o.String())

	opwg := sync.WaitGroup{}
	opwg.Add(maxWorkers)

	for wid := 0; wid < maxWorkers; wid++ {
		workerid := wid
		worker := o.Gen.GetWorker()
		go func() {
			defer opwg.Done()
			o.runWorker(worker, workerid)
		}()
	}

	combinerwg := sync.WaitGroup{}
	combinerwg.Add(1)
	go func() {
		defer combinerwg.Done()
		o.Combiner()
	}()
	opwg.Wait()
	//log.Println("Workers Returned Order Pres")
	close(o.resultQ)
	combinerwg.Wait()
	o.Exit()
	//stop or close here?
	return o.Error()
}
