package main

import (
	"testing"
)

func TestErrRegexp(t *testing.T) {
	var strings = [][]byte{
		// https://nixpkgs-update-logs.nix-community.org/grafana-dash-n-grab/2024-09-13.log
		[]byte(`grafana-dash-n-grab 0.6.0 -> 0.7.1 https://github.com/esnet/gdg/releases
attrpath: grafana-dash-n-grab
Checking auto update branch...
No auto update branch exists
[version] 
[version] generic version rewriter does not support multiple hashes
[rustCrateVersion] 
[rustCrateVersion] No cargoSha256 or cargoHash found
[golangModuleVersion] 
[golangModuleVersion] Found old vendorHash = "sha256-XJSi+p++1QFfGk57trfIgyv0nWUm38H0n/qbJgV8lEM="
build succeeded unexpectedlystderr did not split as expected full stderr was: 
error: attribute 'originalSrc' in selection path 'grafana-dash-n-grab.originalSrc' not found
stderr did not split as expected full stderr was: 
these 2 derivations will be built:
  /nix/store/l87krfaqq1fp6669pmd6caajrlxph771-grafana-dash-n-grab-0.7.1-go-modules.drv
  /nix/store/81w575wxm8kx03v65zaafv22gga34fir-grafana-dash-n-grab-0.7.1.drv
building '/nix/store/l87krfaqq1fp6669pmd6caajrlxph771-grafana-dash-n-grab-0.7.1-go-modules.drv'...
Running phase: unpackPhase
unpacking source archive /nix/store/vw7kj6qslavh8fq9jrc6k6gflisafvk9-source
source root is source
Running phase: patchPhase
Running phase: updateAutotoolsGnuConfigScriptsPhase
Running phase: configurePhase
Running phase: buildPhase
go: go.mod requires go >= 1.23.0 (running go 1.22.7; GOTOOLCHAIN=local)
error: builder for '/nix/store/l87krfaqq1fp6669pmd6caajrlxph771-grafana-dash-n-grab-0.7.1-go-modules.drv' failed with exit code 1;
       last 8 log lines:
       > Running phase: unpackPhase
       > unpacking source archive /nix/store/vw7kj6qslavh8fq9jrc6k6gflisafvk9-source
       > source root is source
       > Running phase: patchPhase
       > Running phase: updateAutotoolsGnuConfigScriptsPhase
       > Running phase: configurePhase
       > Running phase: buildPhase
       > go: go.mod requires go >= 1.23.0 (running go 1.22.7; GOTOOLCHAIN=local)
       For full logs, run 'nix log /nix/store/l87krfaqq1fp6669pmd6caajrlxph771-grafana-dash-n-grab-0.7.1-go-modules.drv'.
error: 1 dependencies of derivation '/nix/store/81w575wxm8kx03v65zaafv22gga34fir-grafana-dash-n-grab-0.7.1.drv' failed to build

`),
		// https://nixpkgs-update-logs.nix-community.org/babashka/2024-09-13.log
		[]byte(`babashka 1.3.191 -> 1.4.192 https://github.com/babashka/babashka/releases
attrpath: babashka
Checking auto update branch...
No auto update branch exists
[version] 
Received ExitFailure 1 when running
Raw command: /nix/store/lv617x6hjgkirm682kxx8zcfs2bc4j00-nix-2.18.5/bin/nix --extra-experimental-features nix-command --extra-experimental-features flakes eval .#babashka.src --raw --apply "p: p.drvAttrs.outputHash"
Standard error:


[K
[Kerror: flake 'git+file:///var/cache/nixpkgs-update/worker/worktree/babashka' does not provide attribute 'packages.x86_64-linux.babashka.src', 'legacyPackages.x86_64-linux.babashka.src' or 'babashka.src'
Received ExitFailure 1 when running
Raw command: /nix/store/lv617x6hjgkirm682kxx8zcfs2bc4j00-nix-2.18.5/bin/nix --extra-experimental-features nix-command --extra-experimental-features flakes eval .#babashka.originalSrc --raw --apply "p: p.drvAttrs.outputHash"
Standard error:


[K
[Kerror: flake 'git+file:///var/cache/nixpkgs-update/worker/worktree/babashka' does not provide attribute 'packages.x86_64-linux.babashka.originalSrc', 'legacyPackages.x86_64-linux.babashka.originalSrc' or 'babashka.originalSrc'
Received ExitFailure 1 when running
Raw command: /nix/store/lv617x6hjgkirm682kxx8zcfs2bc4j00-nix-2.18.5/bin/nix --extra-experimental-features nix-command --extra-experimental-features flakes eval .#babashka --raw --apply "p: p.drvAttrs.outputHash"
Standard error:


[K
[Kerror: attribute 'outputHash' missing

       at Â«stringÂ»:1:4:

            1| p: p.drvAttrs.outputHash
             |    ^

`),
		// https://nixpkgs-update-logs.nix-community.org/php83Extensions.ssh2/2024-09-19.log
		[]byte(`php83Extensions.ssh2 1.3.1 -> 1.4.1 https://github.com/php/pecl-networking-ssh2/releases
attrpath: php83Extensions.ssh2
Checking auto update branch...
[version] 
[version] skipping because derivation has updateScript
[rustCrateVersion] 
[rustCrateVersion] No cargoSha256 or cargoHash found
[golangModuleVersion] 
[golangModuleVersion] Not a buildGoModule package with vendorSha256 or vendorHash
[npmDepsVersion] 
[npmDepsVersion] No npmDepsHash
[updateScript] 
[updateScript] Failed with exit code 1
this derivation will be built:
  /nix/store/f4n3rravmihnwjyxk1dkmz88mncl00pd-packages.json.drv
building '/nix/store/f4n3rravmihnwjyxk1dkmz88mncl00pd-packages.json.drv'...

Going to be running update for following packages:
 - php-ssh2-1.3.1

Press Enter key to continue...
Running update for:
 - php-ssh2-1.3.1: UPDATING ...
 - php-ssh2-1.3.1: ERROR

--- SHOWING ERROR LOG FOR php-ssh2-1.3.1 ----------------------

Traceback (most recent call last):
  File "/nix/store/xwqs3h76fvc0b5ibzngccakgp136h0r2-nix-update-1.5.1/bin/.nix-update-wrapped", line 9, in <module>
    sys.exit(main())
             ^^^^^^
  File "/nix/store/xwqs3h76fvc0b5ibzngccakgp136h0r2-nix-update-1.5.1/lib/python3.12/site-packages/nix_update/__init__.py", line 303, in main
    package = update(options)
              ^^^^^^^^^^^^^^^
  File "/nix/store/xwqs3h76fvc0b5ibzngccakgp136h0r2-nix-update-1.5.1/lib/python3.12/site-packages/nix_update/update.py", line 440, in update
    update_hash = update_version(
                  ^^^^^^^^^^^^^^^
  File "/nix/store/xwqs3h76fvc0b5ibzngccakgp136h0r2-nix-update-1.5.1/lib/python3.12/site-packages/nix_update/update.py", line 375, in update_version
    new_version = fetch_latest_version(
                  ^^^^^^^^^^^^^^^^^^^^^
  File "/nix/store/xwqs3h76fvc0b5ibzngccakgp136h0r2-nix-update-1.5.1/lib/python3.12/site-packages/nix_update/version/__init__.py", line 138, in fetch_latest_version
    raise VersionError(
nix_update.errors.VersionError: Please specify the version. We can only get the latest version from codeberg/crates.io/gitea/github/gitlab/pypi/savannah/sourcehut/rubygems/npm projects right now


--- SHOWING ERROR LOG FOR php-ssh2-1.3.1 ----------------------
The update script for php-ssh2-1.3.1 failed with exit code 1

`),
		// https://nixpkgs-update-logs.nix-community.org/kyverno-chainsaw/2024-09-19.log
		[]byte(`kyverno-chainsaw 0 -> 1
attrpath: kyverno-chainsaw
Checking auto update branch...
[version] 
[version] generic version rewriter does not support multiple hashes
[rustCrateVersion] 
[rustCrateVersion] No cargoSha256 or cargoHash found
[golangModuleVersion] 
[golangModuleVersion] skipping because derivation has updateScript
[npmDepsVersion] 
[npmDepsVersion] No npmDepsHash
[updateScript] 
[updateScript] Failed with exit code 1
this derivation will be built:
  /nix/store/bgvga0xfaf3yqnrkil0b9756cr19vmmd-packages.json.drv
building '/nix/store/bgvga0xfaf3yqnrkil0b9756cr19vmmd-packages.json.drv'...

Going to be running update for following packages:
 - kyverno-chainsaw-0.2.8

Press Enter key to continue...
Running update for:
 - kyverno-chainsaw-0.2.8: UPDATING ...
 - kyverno-chainsaw-0.2.8: ERROR

--- SHOWING ERROR LOG FOR kyverno-chainsaw-0.2.8 ----------------------

warning: found empty hash, assuming 'sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA='
this derivation will be built:
  /nix/store/gxvr06ifbpw342msbqbjd89fv8572kdr-kyverno-chainsaw-0.2.10-go-modules.drv
building '/nix/store/gxvr06ifbpw342msbqbjd89fv8572kdr-kyverno-chainsaw-0.2.10-go-modules.drv'...
Running phase: unpackPhase
unpacking source archive /nix/store/dfg7vz1wcbhzbwpx890slcpl33cqa6pw-source
source root is source
Running phase: patchPhase
Running phase: updateAutotoolsGnuConfigScriptsPhase
Running phase: configurePhase
Running phase: buildPhase
go: go.mod requires go >= 1.23.0 (running go 1.22.7; GOTOOLCHAIN=local)
error: builder for '/nix/store/gxvr06ifbpw342msbqbjd89fv8572kdr-kyverno-chainsaw-0.2.10-go-modules.drv' failed with exit code 1;
       last 8 log lines:
       > Running phase: unpackPhase
       > unpacking source archive /nix/store/dfg7vz1wcbhzbwpx890slcpl33cqa6pw-source
       > source root is source
       > Running phase: patchPhase
       > Running phase: updateAutotoolsGnuConfigScriptsPhase
       > Running phase: configurePhase
       > Running phase: buildPhase
       > go: go.mod requires go >= 1.23.0 (running go 1.22.7; GOTOOLCHAIN=local)
       For full logs, run 'nix log /nix/store/gxvr06ifbpw342msbqbjd89fv8572kdr-kyverno-chainsaw-0.2.10-go-modules.drv'.
Traceback (most recent call last):
  File "/nix/store/xwqs3h76fvc0b5ibzngccakgp136h0r2-nix-update-1.5.1/bin/.nix-update-wrapped", line 9, in <module>
    sys.exit(main())
             ^^^^^^
  File "/nix/store/xwqs3h76fvc0b5ibzngccakgp136h0r2-nix-update-1.5.1/lib/python3.12/site-packages/nix_update/__init__.py", line 303, in main
    package = update(options)
              ^^^^^^^^^^^^^^^
  File "/nix/store/xwqs3h76fvc0b5ibzngccakgp136h0r2-nix-update-1.5.1/lib/python3.12/site-packages/nix_update/update.py", line 450, in update
    update_go_modules_hash(opts, package.filename, package.go_modules)
  File "/nix/store/xwqs3h76fvc0b5ibzngccakgp136h0r2-nix-update-1.5.1/lib/python3.12/site-packages/nix_update/update.py", line 151, in update_go_modules_hash
    target_hash = nix_prefetch(opts, "goModules")
                  ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/nix/store/xwqs3h76fvc0b5ibzngccakgp136h0r2-nix-update-1.5.1/lib/python3.12/site-packages/nix_update/update.py", line 128, in nix_prefetch
    raise UpdateError(
nix_update.errors.UpdateError: failed to retrieve hash when trying to update kyverno-chainsaw.goModules


--- SHOWING ERROR LOG FOR kyverno-chainsaw-0.2.8 ----------------------
The update script for kyverno-chainsaw-0.2.8 failed with exit code 1
`),
	}
	for _, s := range strings {
		if errRE.Find(s) == nil {
			t.Errorf("error not caught: %s", s)
		}
	}
}
