{
  description = "Voxora development tooling";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    miqtSrc = {
      url = "github:rcalixte/miqt/qt_611";
      flake = false;
    };
  };

  outputs = { self, nixpkgs, miqtSrc }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs {
        inherit system;
        config.allowUnfree = true;
        config.android_sdk.accept_license = true;
      };
      lib = pkgs.lib;
      pkgConfig = pkgs."pkg-config";
      mesaDemos = pkgs."mesa-demos";
      nixFormatter = pkgs.nixfmt;
      dockerPackage = pkgs.docker;

      goTool = if pkgs ? go_1_26 then pkgs.go_1_26 else pkgs.go;
      qtPackages = with pkgs.qt6; [
        qtbase
        qtdeclarative
        qtsvg
      ];
      androidPackages = pkgs.androidenv.composeAndroidPackages {
        buildToolsVersions = [ "33.0.0" ];
        platformVersions = [ "33" ];
        abiVersions = [ "arm64-v8a" ];
        includeNDK = true;
        ndkVersions = [ "25.1.8937393" ];
        cmakeVersions = [ "3.22.1" ];
      };
      androidSdkRoot = "${androidPackages.androidsdk}/libexec/android-sdk";
      androidNdkRoot = "${androidSdkRoot}/ndk-bundle";
      qtPluginPath = lib.concatStringsSep ":" [
        "${pkgs.qt6.qtbase}/lib/qt-6/plugins"
        "${pkgs.qt6.qtdeclarative}/lib/qt-6/plugins"
        "${pkgs.qt6.qtsvg}/lib/qt-6/plugins"
      ];
      qmlImportPath = lib.concatStringsSep ":" [
        "${pkgs.qt6.qtdeclarative}/lib/qt-6/qml"
        "${pkgs.qt6.qtsvg}/lib/qt-6/qml"
      ];
      pkgConfigPath = lib.makeSearchPath "lib/pkgconfig" qtPackages;
      libraryPath = lib.makeLibraryPath (qtPackages ++ [ pkgs.stdenv.cc.cc ]);
      commonEnv = ''
        export CGO_ENABLED=1
        export PKG_CONFIG_PATH="${pkgConfigPath}''${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
        export LD_LIBRARY_PATH="${libraryPath}''${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
        export QT_PLUGIN_PATH="${qtPluginPath}''${QT_PLUGIN_PATH:+:$QT_PLUGIN_PATH}"
        export QML2_IMPORT_PATH="${qmlImportPath}''${QML2_IMPORT_PATH:+:$QML2_IMPORT_PATH}"
      '';
      desktopRunEnv = ''
        hostLibDirs=()
        for dir in /run/opengl-driver/lib /usr/lib64 /usr/lib; do
          if [ -d "$dir" ]; then
            hostLibDirs+=("$dir")
          fi
        done

        if [ "''${#hostLibDirs[@]}" -gt 0 ]; then
          hostLibPath="$(IFS=:; printf '%s' "''${hostLibDirs[*]}")"
          export LD_LIBRARY_PATH="$hostLibPath''${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
        fi
      '';
      androidEnv = ''
        export ANDROID_HOME="${androidSdkRoot}"
        export ANDROID_SDK_ROOT="${androidSdkRoot}"
        export ANDROID_NDK_HOME="${androidNdkRoot}"
        export ANDROID_NDK_ROOT="${androidNdkRoot}"
        export JAVA_HOME="${pkgs.jdk17}"
        export GRADLE_OPTS="-Dorg.gradle.project.android.aapt2FromMavenOverride=${androidSdkRoot}/build-tools/33.0.0/aapt2''${GRADLE_OPTS:+ $GRADLE_OPTS}"
      '';
      repoRootCheck = ''
        if [ ! -f "$PWD/voxora/go.mod" ]; then
          printf '%s\n' "Run this command from the repository root." >&2
          exit 1
        fi
      '';
      androidApkDiscovery = ''
        apkPath=""
        for candidate in "$repoRoot/voxora"/*.apk; do
          [ -e "$candidate" ] || continue
          apkPath="$candidate"
          break
        done

        if [ -z "$apkPath" ]; then
          printf '%s\n' "No APK found in $repoRoot/voxora after the Android build." >&2
          exit 1
        fi
      '';
      androidPackageDiscovery = ''
        packageName="org.qtproject.example.voxora"

        if [ -f "$repoRoot/voxora/android-build/AndroidManifest.xml" ]; then
          while IFS= read -r line; do
            case "$line" in
              *package=\"*\"*)
                packageName=''${line#*package=\"}
                packageName=''${packageName%%\"*}
                break
                ;;
            esac
          done < "$repoRoot/voxora/android-build/AndroidManifest.xml"
        fi
      '';
      androidDeviceDiscovery = ''
        adbPath="$ANDROID_SDK_ROOT/platform-tools/adb"
        if [ ! -x "$adbPath" ]; then
          printf '%s\n' "adb was not found at $adbPath" >&2
          exit 1
        fi

        serial=""
        headerSeen=0
        while IFS=$'\t' read -r deviceSerial deviceState _; do
          if [ "$headerSeen" -eq 0 ]; then
            headerSeen=1
            continue
          fi

          if [ "$deviceState" = "device" ]; then
            serial="$deviceSerial"
            break
          fi
        done < <("$adbPath" devices)

        if [ -z "$serial" ]; then
          printf '%s\n' "No running adb devices or emulators found." >&2
          exit 1
        fi
      '';

      miqtDocker = pkgs.buildGoModule {
        pname = "miqt-docker";
        version = "qt_611";
        src = miqtSrc;
        subPackages = [ "cmd/miqt-docker" ];
        vendorHash = null;
      };

      voxoraRun = pkgs.writeShellApplication {
        name = "voxora-run";
        runtimeInputs = [ goTool pkgConfig pkgs.gcc mesaDemos ] ++ qtPackages;
        text = ''
          set -euo pipefail
          ${repoRootCheck}
          ${commonEnv}

          cd "$PWD/voxora"
          runBin="$(mktemp "$PWD/.voxora-run.XXXXXX")"
          trap 'rm -f "$runBin"' EXIT

          go build -ldflags "-s -w" -o "$runBin" .

          ${desktopRunEnv}
          exec "$runBin" "$@"
        '';
      };

      voxoraDesktopBuild = pkgs.writeShellApplication {
        name = "voxora-desktop-build";
        runtimeInputs = [ goTool pkgConfig pkgs.gcc ] ++ qtPackages;
        text = ''
          set -euo pipefail
          ${repoRootCheck}
          ${commonEnv}

          mkdir -p "$PWD/dist"
          cd "$PWD/voxora"
          exec go build -ldflags "-s -w" -o "$PWD/../dist/voxora" .
        '';
      };

      voxoraAndroidBuild = pkgs.writeShellApplication {
        name = "voxora-android-build";
        runtimeInputs = [ goTool dockerPackage miqtDocker ] ++ qtPackages ++ [ androidPackages.androidsdk pkgs.jdk17 pkgConfig pkgs.gcc ];
        text = ''
          set -euo pipefail
          ${repoRootCheck}
          ${commonEnv}
          ${androidEnv}

          cd "$PWD/voxora"
          exec miqt-docker android-qt6 -android-build "$@"
        '';
      };

      voxoraAndroidLaunch = pkgs.writeShellApplication {
        name = "voxora-android-launch";
        runtimeInputs = [ goTool dockerPackage miqtDocker ] ++ qtPackages ++ [ androidPackages.androidsdk pkgs.jdk17 pkgConfig pkgs.gcc ];
        text = ''
          set -euo pipefail
          ${repoRootCheck}
          ${commonEnv}
          ${androidEnv}
          repoRoot="$PWD"
          ${androidDeviceDiscovery}

          cd "$repoRoot/voxora"
          miqt-docker android-qt6 -android-build "$@"
          cd "$repoRoot"
          ${androidApkDiscovery}
          ${androidPackageDiscovery}
          "$adbPath" -s "$serial" install -r "$apkPath"
          exec "$adbPath" -s "$serial" shell monkey -p "$packageName" -c android.intent.category.LAUNCHER 1
        '';
      };

      app = program: description: {
        type = "app";
        inherit program;
        meta.description = description;
      };
    in
    {
      formatter.${system} = nixFormatter;

      packages.${system} = {
        default = voxoraRun;
        run = voxoraRun;
        "desktop-build" = voxoraDesktopBuild;
        "android-build" = voxoraAndroidBuild;
        "android-launch" = voxoraAndroidLaunch;
        "miqt-docker" = miqtDocker;
      };

      apps.${system} = {
        default = app "${voxoraRun}/bin/voxora-run" "Run the desktop app using host GL libraries";
        run = app "${voxoraRun}/bin/voxora-run" "Run the desktop app using host GL libraries";
        "desktop-build" = app "${voxoraDesktopBuild}/bin/voxora-desktop-build" "Build the desktop binary into dist/";
        "android-build" = app "${voxoraAndroidBuild}/bin/voxora-android-build" "Build the Android APK";
        "android-launch" = app "${voxoraAndroidLaunch}/bin/voxora-android-launch" "Install and launch the Android app on the first adb device";
        "miqt-docker" = app "${miqtDocker}/bin/miqt-docker" "Run the raw miqt-docker helper";
      };

      devShells.${system}.default = pkgs.mkShell {
        packages = [
          goTool
          dockerPackage
          pkgs.jdk17
          nixFormatter
          mesaDemos
          pkgConfig
          pkgs.gcc
          androidPackages.androidsdk
          miqtDocker
        ] ++ qtPackages;

        shellHook = ''
          ${commonEnv}
          ${androidEnv}

          printf '%s\n' "Available commands: nix run .#run, nix run .#desktop-build, nix run .#android-build, nix run .#android-launch, nix run .#miqt-docker"
        '';
      };
    };
}
