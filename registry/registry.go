package registry

type Function interface {
	Process(event interface{})
}

var functions []Function

func Register(f Function) {
	functions = append(functions, f)
}

func Dispatch(event interface{}) {
	for _, f := range functions {
		f.Process(event)
	}
}
