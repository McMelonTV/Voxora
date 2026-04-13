//go:build android

package authbridge

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>

static int voxora_load_ffmpeg_cli_so() {
    void* h = dlopen("libffmpeg_cli.so", RTLD_NOW | RTLD_GLOBAL);
    if (h == NULL) {
        return 0;
    }
    return 1;
}
*/
import "C"

func ensureFFmpegNativeLoaded() bool {
	return C.voxora_load_ffmpeg_cli_so() == 1
}
