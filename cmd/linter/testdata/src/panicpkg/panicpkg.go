package panicpkg

var (
	_ = bad
	_ = shadowed
)

func bad() {
	panic("boom") // want "panic call is forbidden"
}

func shadowed() {

	panic := func(any) {}
	panic("ok")
}
