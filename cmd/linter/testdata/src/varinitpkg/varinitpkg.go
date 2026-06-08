package varinitpkg

import "os"

var _ = func() int {
	panic("boom") // want "panic call is forbidden"
	return 0
}()

var _ = func() int {
	os.Exit(1) // want "os\\.Exit must only be called from main\\.main"
	return 0
}()
