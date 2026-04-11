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
      qtToolPath = lib.concatStringsSep ":" [
        "${pkgs.qt6.qtbase}/libexec"
        "${pkgs.qt6.qtdeclarative}/libexec"
      ];
      androidPackages = pkgs.androidenv.composeAndroidPackages {
        buildToolsVersions = [ "36.0.0" ];
        platformVersions = [ "36" ];
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
        export PATH="${qtToolPath}:$PATH"
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
        export GRADLE_OPTS="-Dorg.gradle.project.android.aapt2FromMavenOverride=${androidSdkRoot}/build-tools/36.0.0/aapt2''${GRADLE_OPTS:+ $GRADLE_OPTS}"
      '';
      repoRootCheck = ''
        if [ ! -f "$PWD/voxora/go.mod" ]; then
          printf '%s\n' "Run this command from the repository root." >&2
          exit 1
        fi
      '';

      miqtDocker = pkgs.buildGoModule {
        pname = "miqt-docker";
        version = "qt_611";
        src = miqtSrc;
        subPackages = [ "cmd/miqt-docker" ];
        vendorHash = null;
        nativeBuildInputs = [ pkgs.gnused ];
        postPatch = ''
          script=cmd/miqt-docker/android-build.sh

          sed -i 's/^\([[:space:]]*\)echo Qt6Widgets[[:space:]]*$/\1echo Qt6Widgets\n\1echo Qt6Gui\n\1echo Qt6Qml\n\1echo Qt6Quick/' "$script"
          sed -i '/plugins\/platforms -lplugins_platforms_qtforandroid_arm64-v8a/d' "$script"

          grep -n 'echo Qt6Gui\|echo Qt6Qml\|echo Qt6Quick\|plugins_platforms_qtforandroid' "$script"
        '';
      };

      miqtRcc = pkgs.buildGoModule {
        pname = "miqt-rcc";
        version = "qt_611";
        src = miqtSrc;
        subPackages = [ "cmd/miqt-rcc" ];
        vendorHash = null;
        preCheck = ''
          export PATH="${pkgs.qt6.qtbase}/libexec:$PATH"
        '';
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

          if [ -f "deployment-settings.json" ]; then
            qtTargetPath="$(sed -n 's/^[[:space:]]*"qt"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' deployment-settings.json | head -n 1)"
            if [ -n "$qtTargetPath" ] && [[ "$qtTargetPath" == */android_* ]]; then
              qtRoot="''${qtTargetPath%/android_*}"
              qtHostPath="$qtRoot/gcc_64"

              # Ensure host-side Qt tools are discoverable for androiddeployqt.
              export QT_HOST_PATH="$qtHostPath"
              export QT_HOST_BINS="$qtHostPath/bin"
              export PATH="$qtHostPath/libexec:$qtHostPath/bin:$PATH"

              # Keep QML/plugin discovery deterministic inside containerized builds.
              export QML2_IMPORT_PATH="$qtTargetPath/qml:$qtHostPath/qml''${QML2_IMPORT_PATH:+:$QML2_IMPORT_PATH}"
              export QT_PLUGIN_PATH="$qtTargetPath/plugins:$qtHostPath/plugins''${QT_PLUGIN_PATH:+:$QT_PLUGIN_PATH}"
              export QT_QPA_PLATFORM_PLUGIN_PATH="$qtTargetPath/plugins/platforms"
            fi
          fi

          exec miqt-docker android-qt6 -android-build "$@"
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
        "miqt-docker" = miqtDocker;
        "miqt-rcc" = miqtRcc;
      };

      apps.${system} = {
        default = app "${voxoraRun}/bin/voxora-run" "Run the desktop app using host GL libraries";
        run = app "${voxoraRun}/bin/voxora-run" "Run the desktop app using host GL libraries";
        "desktop-build" = app "${voxoraDesktopBuild}/bin/voxora-desktop-build" "Build the desktop binary into dist/";
        "android-build" = app "${voxoraAndroidBuild}/bin/voxora-android-build" "Build the Android APK";
        "miqt-docker" = app "${miqtDocker}/bin/miqt-docker" "Run the raw miqt-docker helper";
        "miqt-rcc" = app "${miqtRcc}/bin/miqt-rcc" "Generate MIQT Qt resource wrappers";
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
          miqtRcc
        ] ++ qtPackages;

        shellHook = ''
          ${commonEnv}
          ${androidEnv}

          printf '%s\n' "Available commands: nix run .#run, nix run .#desktop-build, nix run .#android-build, nix run .#miqt-docker, nix run .#miqt-rcc"
        '';
      };
    };
}
