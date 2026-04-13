package go_librespot

import (
	"os"
	"runtime"
	"strings"

	spotifypb "github.com/devgianlu/go-librespot/proto/spotify"
	clienttokenpb "github.com/devgianlu/go-librespot/proto/spotify/clienttoken/data/v0"
)

func androidClientTokenProfile() string {
	profile := strings.ToLower(strings.TrimSpace(os.Getenv("LIBRESPOT_ANDROID_CLIENTTOKEN_PROFILE")))
	switch profile {
	case "android", "native":
		return "android"
	case "linux", "desktop", "desktop-linux":
		return "linux"
	default:
		// Android native clienttoken payloads are currently rejected in Voxora's
		// runtime environment with HTTP 400. Use desktop-linux profile by default
		// so bootstrap/session creation matches known working desktop behavior.
		return "linux"
	}
}

func androidAPProfile() string {
	profile := strings.ToLower(strings.TrimSpace(os.Getenv("LIBRESPOT_ANDROID_AP_PROFILE")))
	switch profile {
	case "android", "native":
		return "android"
	case "linux", "desktop", "desktop-linux":
		return "linux"
	default:
		// Match desktop behavior by default to avoid Android-only AP token
		// authentication failures (TryAnotherAP loops).
		return "linux"
	}
}

func GetOS() spotifypb.Os {
	switch runtime.GOOS {
	case "android":
		if androidAPProfile() != "android" {
			return spotifypb.Os_OS_LINUX
		}
		return spotifypb.Os_OS_ANDROID
	case "darwin":
		return spotifypb.Os_OS_OSX
	case "freebsd":
		return spotifypb.Os_OS_FREEBSD
	case "ios":
		return spotifypb.Os_OS_IPHONE
	case "linux":
		return spotifypb.Os_OS_LINUX
	case "windows":
		return spotifypb.Os_OS_WINDOWS
	default:
		return spotifypb.Os_OS_UNKNOWN
	}
}

func GetCpuFamily() spotifypb.CpuFamily {
	switch runtime.GOARCH {
	case "386":
		return spotifypb.CpuFamily_CPU_X86
	case "amd64":
		return spotifypb.CpuFamily_CPU_X86_64
	case "arm":
		return spotifypb.CpuFamily_CPU_ARM
	case "arm64":
		return spotifypb.CpuFamily_CPU_ARM
	case "mips":
		return spotifypb.CpuFamily_CPU_MIPS
	case "mips64":
		return spotifypb.CpuFamily_CPU_MIPS
	case "ppc64":
		return spotifypb.CpuFamily_CPU_PPC_64
	default:
		return spotifypb.CpuFamily_CPU_UNKNOWN
	}
}

func GetPlatform() spotifypb.Platform {
	switch runtime.GOOS {
	case "android":
		if androidAPProfile() != "android" {
			return spotifypb.Platform_PLATFORM_LINUX_ARM
		}
		return spotifypb.Platform_PLATFORM_ANDROID_ARM
	case "darwin":
		switch runtime.GOARCH {
		case "386":
			return spotifypb.Platform_PLATFORM_OSX_X86
		case "amd64":
			return spotifypb.Platform_PLATFORM_OSX_X86_64
		case "ppc64":
			return spotifypb.Platform_PLATFORM_OSX_PPC
		}
	case "freebsd":
		switch runtime.GOARCH {
		case "386":
			return spotifypb.Platform_PLATFORM_FREEBSD_X86
		case "amd64":
			return spotifypb.Platform_PLATFORM_FREEBSD_X86_64
		}
	case "ios":
		switch runtime.GOARCH {
		case "arm":
			return spotifypb.Platform_PLATFORM_IPHONE_ARM
		case "arm64":
			return spotifypb.Platform_PLATFORM_IPHONE_ARM64
		}
	case "linux":
		switch runtime.GOARCH {
		case "386":
			return spotifypb.Platform_PLATFORM_LINUX_X86
		case "amd64":
			return spotifypb.Platform_PLATFORM_LINUX_X86_64
		case "mips":
			return spotifypb.Platform_PLATFORM_LINUX_MIPS
		case "mips64":
			return spotifypb.Platform_PLATFORM_LINUX_MIPS
		case "arm":
			return spotifypb.Platform_PLATFORM_LINUX_ARM
		case "arm64":
			return spotifypb.Platform_PLATFORM_LINUX_ARM
		}
	case "windows":
		switch runtime.GOARCH {
		case "386":
			return spotifypb.Platform_PLATFORM_WIN32_X86
		case "amd64":
			return spotifypb.Platform_PLATFORM_WIN32_X86_64
		case "arm":
			return spotifypb.Platform_PLATFORM_WINDOWS_CE_ARM
		case "arm64":
			return spotifypb.Platform_PLATFORM_WINDOWS_CE_ARM
		}
	case "js":
		return spotifypb.Platform_PLATFORM_WEBPLAYER
	}

	return spotifypb.Platform_PLATFORM_GENERIC_PARTNER
}

func GetPlatformSpecificData() *clienttokenpb.PlatformSpecificData {
	switch runtime.GOOS {
	case "android":
		if androidClientTokenProfile() != "android" {
			return &clienttokenpb.PlatformSpecificData{
				Data: &clienttokenpb.PlatformSpecificData_DesktopLinux{
					DesktopLinux: &clienttokenpb.NativeDesktopLinuxData{},
				},
			}
		}
		return &clienttokenpb.PlatformSpecificData{
			Data: &clienttokenpb.PlatformSpecificData_Android{
				Android: &clienttokenpb.NativeAndroidData{
					ScreenDimensions: &clienttokenpb.Screen{
						Width:          1080,
						Height:         2400,
						Density:        420,
						UnknownValue_4: 0,
						UnknownValue_5: 0,
					},
					AndroidVersion: "14",
					ApiVersion:     34,
					DeviceName:     "Android",
					ModelStr:       "Android",
					Vendor:         "Google",
					Vendor_2:       "Google",
					UnknownValue_8: 0,
				},
			},
		}
	case "darwin":
		return &clienttokenpb.PlatformSpecificData{
			Data: &clienttokenpb.PlatformSpecificData_DesktopMacos{
				DesktopMacos: &clienttokenpb.NativeDesktopMacOSData{},
			},
		}
	case "ios":
		return &clienttokenpb.PlatformSpecificData{
			Data: &clienttokenpb.PlatformSpecificData_Ios{
				Ios: &clienttokenpb.NativeIOSData{},
			},
		}
	case "linux", "freebsd":
		return &clienttokenpb.PlatformSpecificData{
			Data: &clienttokenpb.PlatformSpecificData_DesktopLinux{
				DesktopLinux: &clienttokenpb.NativeDesktopLinuxData{},
			},
		}
	case "windows":
		return &clienttokenpb.PlatformSpecificData{
			Data: &clienttokenpb.PlatformSpecificData_DesktopWindows{
				DesktopWindows: &clienttokenpb.NativeDesktopWindowsData{},
			},
		}
	}

	return nil
}
