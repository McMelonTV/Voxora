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

      goTool = if pkgs ? go_1_26 then pkgs.go_1_26 else pkgs.go;
      qtPackages = with pkgs.qt6; [
        qtbase
        qtdeclarative
        qtsvg
      ];
      qtAndroidVersion = "6.10.2";
      qtAndroidSrc = pkgs.fetchurl {
        url = "https://download.qt.io/official_releases/qt/6.10/6.10.2/single/qt-everywhere-src-6.10.2.tar.xz";
        hash = "sha256-w98PDkIRMMxS7YHLcSNYgERxzpvSpB2Xgo+fWxv3/tI=";
      };
      qtHostToolsRoot = pkgs.symlinkJoin {
        name = "qt6-host-tools";
        paths = qtPackages ++ [ pkgs.qt6.qtshadertools pkgs.qt6.qttools pkgs.qt6.qttranslations ];
      };
      mkQtAndroidRuntimeFull = {
        abi,
        eglLibrary,
        glesv2Library,
      }:
        pkgs.stdenv.mkDerivation {
          pname = "qt-android-runtime-full-${lib.replaceStrings ["-"] ["_"] abi}";
          version = qtAndroidVersion;
          src = qtAndroidSrc;
          nativeBuildInputs = [
            pkgs.bison
            pkgs.cmake
            pkgs.flex
            pkgs.gperf
            pkgs.jdk17
            pkgs.ninja
            pkgs.perl
            pkgs.python3
            pkgs.pkg-config
            pkgs.git
            pkgs.which
          ];
          configurePhase = ''
            runHook preConfigure

            export ANDROID_SDK_ROOT="${androidSdkRoot}"
            export ANDROID_NDK_ROOT="${androidNdkRoot}"
            export JAVA_HOME="${pkgs.jdk17}"
            export CMAKE_GENERATOR=Ninja

            mkdir build
            cd build

            ../configure \
              -prefix "$out" \
              -opensource \
              -confirm-license \
              -qt-host-path "${qtHostToolsRoot}" \
              -android-sdk "$ANDROID_SDK_ROOT" \
              -android-ndk "$ANDROID_NDK_ROOT" \
              -android-abis ${abi} \
              -submodules qtbase,qtdeclarative,qtshadertools,qtsvg,qttranslations \
              -release \
              -nomake tests \
              -nomake examples \
              -android-javac-source 8 \
              -android-javac-target 8 \
              -- \
              -DANDROID_PLATFORM=android-${androidMinSdkVersion} \
              -DEGL_INCLUDE_DIR="${androidSysrootIncludeDir}" \
              -DEGL_LIBRARY="${eglLibrary}" \
              -DGLESv2_INCLUDE_DIR="${androidSysrootIncludeDir}" \
              -DGLESv2_LIBRARY="${glesv2Library}" \
              -DINPUT_opengl=es2 \
              -DQT_BUILD_TESTS=OFF \
              -DQT_BUILD_EXAMPLES=OFF \
              -DCMAKE_BUILD_TYPE=Release \
              -DCMAKE_SHARED_LINKER_FLAGS="-Wl,-z,max-page-size=16384 -Wl,-z,common-page-size=16384" \
              -DCMAKE_MODULE_LINKER_FLAGS="-Wl,-z,max-page-size=16384 -Wl,-z,common-page-size=16384" \
              -DCMAKE_EXE_LINKER_FLAGS="-Wl,-z,max-page-size=16384 -Wl,-z,common-page-size=16384"

            runHook postConfigure
          '';
          buildPhase = ''
            runHook preBuild
            cmake --build . --parallel "$NIX_BUILD_CORES"
            runHook postBuild
          '';
          installPhase = ''
            runHook preInstall
            cmake --install .
            runHook postInstall
          '';
        };
      qtAndroidArm64RootFull = mkQtAndroidRuntimeFull {
        abi = "arm64-v8a";
        eglLibrary = "${androidArm64SysrootLibDir}/libEGL.so";
        glesv2Library = "${androidArm64SysrootLibDir}/libGLESv2.so";
      };
      qtAndroidX8664RootFull = mkQtAndroidRuntimeFull {
        abi = "x86_64";
        eglLibrary = "${androidX8664SysrootLibDir}/libEGL.so";
        glesv2Library = "${androidX8664SysrootLibDir}/libGLESv2.so";
      };
      qtAndroidRootFull = pkgs.runCommand "qt-android-runtime-full-${qtAndroidVersion}" {} ''
        cp -R "${qtAndroidArm64RootFull}" "$out"
        chmod -R u+w "$out"

        cd "${qtAndroidX8664RootFull}"
        find lib plugins qml -type f \( -name '*x86_64*.so' -o -name '*x86_64*.json' -o -name '*x86_64*.qmltypes' \) -exec cp --parents {} "$out" \;

        chmod -R a-w "$out"
      '';
      qtAndroidRoot = pkgs.runCommand "qt-android-runtime-${qtAndroidVersion}" {} ''
        mkdir -p "$out"
        for entry in include jar lib mkspecs plugins qml src translations; do
          ln -s "${qtAndroidRootFull}/$entry" "$out/$entry"
        done
      '';
      qtAndroidPkgConfig = pkgs.writeShellScriptBin "voxora-android-pkg-config" (builtins.readFile ./tools/android-pkg-config.sh);
      qtToolPath = lib.concatStringsSep ":" [
        "${pkgs.qt6.qtbase}/libexec"
        "${pkgs.qt6.qtdeclarative}/libexec"
      ];
      qtPkgConfigPath = lib.makeSearchPath "lib/pkgconfig" qtPackages;
      androidPackages = pkgs.androidenv.composeAndroidPackages {
        buildToolsVersions = [ "36.0.0" ];
        platformVersions = [ "36" ];
        abiVersions = [ "arm64-v8a" "x86_64" ];
        includeNDK = true;
        ndkVersions = [ "27.2.12479018" ];
      };
      androidSdkRoot = "${androidPackages.androidsdk}/libexec/android-sdk";
      androidNdkRoot = "${androidSdkRoot}/ndk-bundle";
      androidBuildToolsVersion = "36.0.0";
      androidMinSdkVersion = "28";
      androidTargetSdkVersion = "36";
      androidAppId = "ing.boykiss.voxora";
      androidNdkHost = "linux-x86_64";
      androidToolchainRoot = "${androidNdkRoot}/toolchains/llvm/prebuilt/${androidNdkHost}";
      androidSysrootIncludeDir = "${androidToolchainRoot}/sysroot/usr/include";
      androidArm64SysrootLibDir = "${androidToolchainRoot}/sysroot/usr/lib/aarch64-linux-android/${androidMinSdkVersion}";
      androidX8664SysrootLibDir = "${androidToolchainRoot}/sysroot/usr/lib/x86_64-linux-android/${androidMinSdkVersion}";
      androidArm64Cc = "${androidToolchainRoot}/bin/aarch64-linux-android${androidMinSdkVersion}-clang";
      androidArm64Cxx = "${androidToolchainRoot}/bin/aarch64-linux-android${androidMinSdkVersion}-clang++";
      androidArm64Ar = "${androidToolchainRoot}/bin/llvm-ar";
      androidX8664Cc = "${androidToolchainRoot}/bin/x86_64-linux-android${androidMinSdkVersion}-clang";
      androidX8664Cxx = "${androidToolchainRoot}/bin/x86_64-linux-android${androidMinSdkVersion}-clang++";
      androidX8664Ar = "${androidToolchainRoot}/bin/llvm-ar";
      qtPluginPath = lib.concatStringsSep ":" [
        "${pkgs.qt6.qtbase}/lib/qt-6/plugins"
        "${pkgs.qt6.qtdeclarative}/lib/qt-6/plugins"
        "${pkgs.qt6.qtsvg}/lib/qt-6/plugins"
      ];
      qmlImportPath = lib.concatStringsSep ":" [
        "${pkgs.qt6.qtdeclarative}/lib/qt-6/qml"
      ];
      libraryPath = lib.makeLibraryPath (qtPackages ++ [ pkgs.stdenv.cc.cc ]);
      commonEnv = ''
        export CGO_ENABLED=1
        export PKG_CONFIG_PATH="${qtPkgConfigPath}''${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
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
      androidSdkEnv = ''
        export ANDROID_HOME="${androidSdkRoot}"
        export ANDROID_SDK_ROOT="${androidSdkRoot}"
        export ANDROID_NDK_HOME="${androidNdkRoot}"
        export ANDROID_NDK_ROOT="${androidNdkRoot}"
        export ANDROID_NDK_HOST="${androidNdkHost}"
        export ANDROID_MIN_SDK_VERSION="${androidMinSdkVersion}"
        export ANDROID_TARGET_SDK_VERSION="${androidTargetSdkVersion}"
        export ANDROID_SDK_BUILD_TOOLS_REVISION="${androidBuildToolsVersion}"
        export JAVA_HOME="${pkgs.jdk17}"
        export GRADLE_OPTS="-Dorg.gradle.project.android.aapt2FromMavenOverride=${androidSdkRoot}/build-tools/${androidBuildToolsVersion}/aapt2''${GRADLE_OPTS:+ $GRADLE_OPTS}"
        export QT_ANDROID="${qtAndroidRoot}"
        export QT_QML_IMPORTSCANNER="${pkgs.qt6.qtdeclarative}/libexec/qmlimportscanner"
        export QT_RCC_BINARY="${pkgs.qt6.qtbase}/libexec/rcc"
      '';
      androidCrossEnv = ''
        export ANDROID_TOOLCHAIN_ROOT="${androidToolchainRoot}"
        export ANDROID_ARM64_CC="${androidArm64Cc}"
        export ANDROID_ARM64_CXX="${androidArm64Cxx}"
        export ANDROID_ARM64_AR="${androidArm64Ar}"
        export ANDROID_X86_64_CC="${androidX8664Cc}"
        export ANDROID_X86_64_CXX="${androidX8664Cxx}"
        export ANDROID_X86_64_AR="${androidX8664Ar}"
        export QT_ANDROID_PKG_CONFIG="${qtAndroidPkgConfig}/bin/voxora-android-pkg-config"
        unset PKG_CONFIG_PATH
      '';
      repoRootCheck = ''
        if [ ! -f "$PWD/voxora/go.mod" ]; then
          printf '%s\n' "Run this command from the repository root." >&2
          exit 1
        fi
      '';

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
      syncQtResources = ''
        bash ${lib.escapeShellArg (toString ./tools/update-qt-resources.sh)} "$PWD/voxora"
      '';

      voxoraRun = pkgs.writeShellApplication {
        name = "voxora-run";
        runtimeInputs = [ goTool pkgConfig pkgs.gcc mesaDemos miqtRcc ] ++ qtPackages;
        text = ''
          set -euo pipefail
          ${repoRootCheck}
          ${commonEnv}
          ${syncQtResources}

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
        runtimeInputs = [ goTool pkgConfig pkgs.gcc miqtRcc ] ++ qtPackages;
        text = ''
          set -euo pipefail
          ${repoRootCheck}
          ${commonEnv}
          ${syncQtResources}

          mkdir -p "$PWD/dist"
          cd "$PWD/voxora"
          exec go build -ldflags "-s -w" -o "$PWD/../dist/voxora" .
        '';
      };

      voxoraAndroidBuild = pkgs.writeShellApplication {
        name = "voxora-android-build";
        runtimeInputs = [ goTool pkgs.gradle pkgs.jq miqtRcc ] ++ qtPackages ++ [ androidPackages.androidsdk pkgs.jdk17 pkgConfig pkgs.gcc ];
        text = ''
          set -euo pipefail
          ${repoRootCheck}
          ${commonEnv}
          ${androidSdkEnv}
          ${androidCrossEnv}
          ${syncQtResources}

          cd "$PWD/voxora"
          exec bash ${lib.escapeShellArg (toString ./tools/android-build.sh)} "$@"
        '';
      };

      voxoraAndroidRun = pkgs.writeShellApplication {
        name = "voxora-android-run";
        runtimeInputs = [ androidPackages.androidsdk pkgs.coreutils ];
        text = ''
          set -euo pipefail
          ${repoRootCheck}
          ${androidSdkEnv}

          export ANDROID_ADB="${androidSdkRoot}/platform-tools/adb"
          export VOXORA_ANDROID_BUILD_BIN="${voxoraAndroidBuild}/bin/voxora-android-build"
          export VOXORA_ANDROID_PACKAGE_NAME="${androidAppId}"
          export VOXORA_REPO_ROOT="$PWD"

          cd "$PWD/voxora"
          exec bash ${lib.escapeShellArg (toString ./tools/android-run.sh)} "$@"
        '';
      };

      voxoraAndroidKeystoreSetup = pkgs.writeShellApplication {
        name = "voxora-android-keystore-setup";
        runtimeInputs = [ pkgs.jdk17 pkgs.coreutils ];
        text = ''
          set -euo pipefail
          ${repoRootCheck}

          exec bash ${lib.escapeShellArg (toString ./tools/android-keystore-setup.sh)} "$@"
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
        "android-keystore-setup" = voxoraAndroidKeystoreSetup;
        "qt-android-runtime" = qtAndroidRoot;
        "qt-android-runtime-full" = qtAndroidRootFull;
        "android-run" = voxoraAndroidRun;
        "miqt-rcc" = miqtRcc;
      };

      apps.${system} = {
        default = app "${voxoraRun}/bin/voxora-run" "Run the desktop app using host GL libraries";
        run = app "${voxoraRun}/bin/voxora-run" "Run the desktop app using host GL libraries";
        "desktop-build" = app "${voxoraDesktopBuild}/bin/voxora-desktop-build" "Build the desktop binary into dist/";
        "android-build" = app "${voxoraAndroidBuild}/bin/voxora-android-build" "Build the Android APK";
        "android-keystore-setup" = app "${voxoraAndroidKeystoreSetup}/bin/voxora-android-keystore-setup" "Create Android signing keystore and env file";
        "android-run" = app "${voxoraAndroidRun}/bin/voxora-android-run" "Install if needed and launch the Android app over adb";
        "miqt-rcc" = app "${miqtRcc}/bin/miqt-rcc" "Generate MIQT Qt resource wrappers";
      };

      devShells.${system}.default = pkgs.mkShell {
        packages = [
          goTool
          pkgs.jdk17
          nixFormatter
          mesaDemos
          pkgConfig
          pkgs.gcc
          androidPackages.androidsdk
          miqtRcc
        ] ++ qtPackages;

        shellHook = ''
          ${commonEnv}
          ${androidSdkEnv}

          printf '%s\n' "Available commands: nix run .#run, nix run .#desktop-build, nix run .#android-build, nix run .#android-keystore-setup, nix run .#android-run, nix run .#miqt-rcc"
        '';
      };
    };
}
