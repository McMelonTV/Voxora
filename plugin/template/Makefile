PLUGIN_NAME = voxora_template_plugin

$(shell mkdir -p build)

all: linux-amd64 linux-arm64 \
     android-amd64 android-arm64 \
     windows-amd64 windows-arm64

# Linux
linux-amd64:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -buildmode=c-shared -o build/linux/$(PLUGIN_NAME)_linux-amd64.so main.go

linux-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=1 CC=aarch64-linux-gnu-gcc go build -buildmode=c-shared -o build/linux/$(PLUGIN_NAME)_linux-arm64.so main.go

# Windows
windows-amd64:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=/opt/llvm-mingw/llvm-mingw-ucrt/bin/x86_64-w64-mingw32-gcc go build -buildmode=c-shared -o build/windows/$(PLUGIN_NAME)_windows-amd64.dll main.go

windows-arm64:
	GOOS=windows GOARCH=arm64 CGO_ENABLED=1 CC=/opt/llvm-mingw/llvm-mingw-ucrt/bin/aarch64-w64-mingw32-gcc go build -buildmode=c-shared -o build/windows/$(PLUGIN_NAME)_windows-arm64.dll main.go

# Android
android-amd64:
	GOOS=android GOARCH=amd64 CGO_ENABLED=1 CC=/opt/android-ndk/toolchains/llvm/prebuilt/linux-x86_64/bin/x86_64-linux-android35-clang go build -buildmode=c-shared -o build/android/$(PLUGIN_NAME)_android-amd64.so main.go

android-arm64:
	GOOS=android GOARCH=arm64 CGO_ENABLED=1 CC=/opt/android-ndk/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android35-clang go build -buildmode=c-shared -o build/android/$(PLUGIN_NAME)_android-arm64.so main.go

.PHONY: all linux-amd64 linux-arm64 windows-amd64 windows-arm64 android-amd64 android-arm64