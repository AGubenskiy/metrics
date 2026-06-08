package importpkg

import (
	. "log"
	. "os"
)

func bad() {
	Fatal("forbidden") // want "log\\.Fatal must only be called from main\\.main"
	Exit(1)            // want "os\\.Exit must only be called from main\\.main"
}
