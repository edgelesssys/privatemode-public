#!/usr/bin/env nu

def main [version: string, ...targets: string] {
    # Use justfile as a heuristic for being in the project root
    if not ('justfile' | path exists) {
        print "This script must be run from the project root"
        exit 1
    }

    const script_name = (path self | path basename)
    if ($targets | is-empty) {
        print $"Usage: [SKIP_LIBPRIVATEMODE_BUILD=0] ($script_name) <version> <targets...>"
        print $"Example: ($script_name) 1.0.0 rpm,deb"
        print "Available targets: rpm, deb, dmg, msix"
        exit 1
    }

    let version = ($version | str replace --regex '^v' '')
    let targets = ($targets | str join ' ')
    print $"Building app version ($version) for targets: ($targets)"

    let skip_build = ($env.SKIP_LIBPRIVATEMODE_BUILD? | default '0')
    if $skip_build != '1' {
        mkdir build-libprivatemode/lib build-libprivatemode/include
        nix build .#libprivatemode --out-link build-libprivatemode-tmp
        cp -r build-libprivatemode-tmp/* build-libprivatemode
        chmod +w build-libprivatemode/**/*
        rm -rf build-libprivatemode-tmp
    } else if not ('build-libprivatemode' | path exists) {
        print "SKIP_LIBPRIVATEMODE_BUILD is set, but build-libprivatemode/ does not exist."
        print "Either unset SKIP_LIBPRIVATEMODE_BUILD or build libprivatemode first."
        exit 1
    }
    chmod -R +w build-libprivatemode

    # Frontend build
    cd app/frontend
    npm i
    npm run build
    cd ../..

    # Backend build
    cd app/backend
    npm i
    cp -r ../frontend/build/* ./src/renderer

    # The file doesn't exist on Windows for some reason
    if (sys host | get name) != 'Windows' {
        # Work around electron-installer-redhat being incompatible with RPM 4.20+
        # See: https://github.com/electron/forge/issues/3701
        open node_modules/electron-installer-redhat/resources/spec.ejs
            | str replace --all 'usr/*' '../usr/.'
            | save -f node_modules/electron-installer-redhat/resources/spec.ejs
    }

    open --raw ./package.json
        | str replace '1.0.0' $version
        | save -f ./package.json

    npm run make -- --targets $targets

    open --raw ./package.json
        | str replace $version '1.0.0'
        | save -f ./package.json

    open --raw ./package-lock.json
        | str replace $version '1.0.0'
        | save -f ./package-lock.json

    cd ../..
}
