package opt

type Opt[T any] struct {
	x       T
	present bool
}

func (o Opt[T]) IsPresent() bool {
	return o.present
}

func (o Opt[T]) Value() T {
	return o.x
}

func Some[T any](x T) Opt[T] {
	return Opt[T]{
		x:       x,
		present: true,
	}
}

func None[T any]() Opt[T] {
	return Opt[T]{}
}
