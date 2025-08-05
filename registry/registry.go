package registry

type Function interface {
	Process(event interface{})
}

// 保存所有注册函数
var functions []Function

func Register(f Function) {
	functions = append(functions, f)
}

func Dispatch(event interface{}) {
	for _, f := range functions {
		f.Process(event)
	}
}
