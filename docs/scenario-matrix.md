# Lifecycle scenario matrix

`scripts/scenarios.json` is the executable registry: each entry names its test,
package, states, proof level and applicable platforms. `scenario_report.py`
selects these exact tests, runs them with the race detector and without cached
results, and checks the original Go JSON events. A missing test, package failure,
skipped required test or skipped subtest fails the profile. An unsupported
platform is reported separately and never counted as passed. A profile with no
runnable scenarios fails.

## Commands and proof levels

| Command | Profile | What it proves | Prerequisites |
|---|---|---|---|
| `make test-scenarios` | `fixture`, 31 groups | Real application operations against local HTTP servers, controlled executables and temporary files | Go, Python 3, POSIX shell/make for source backend fixtures; no upstream downloads |
| `make test-runtime-scenarios` | `runtime-fixture`, 6 groups | Real PHP startup, PHAR execution and module loading; archives and modules are test fixtures | Linux PHP with Phar, matching PHP SDK, C compiler/make; Composer/ini groups also support macOS |
| `make test-native-scenarios` | `native`, 2 groups | Official Composer download/update and PECL metadata/archive validation, genuine redis compilation and PHP loading | Linux, network, PHP with Phar, matching phpize/php-config, cc/make/autoconf |
| `make test-native-build` | `native-build`, 1 group | Real PHP source configure/make/install, staged publication, version/API and ready metadata | Linux build dependencies, supplied PHP archive/version/SHA256 |

Counts are top-level scenario groups, including their subtests. They are not
counts of every individual assertion or every test in the repository.

Reports go to ignored `reports/scenarios-<profile>-<platform>.json`, with raw
`.jsonl` Go test events alongside them. Reports record `pass`, `fail`, `partial`
(required test or subtest skipped), `missing` or `unsupported`, plus Go exit
status and the states from the registry. Tests of the report checker reject
silent skips, empty selections and missing tests.

The `Lifecycle scenarios` workflow runs the fixture, runtime fixture and native
profiles on **every PR and push**, and uploads evidence even on failure. Native
tools are provisioned on the isolated Ubuntu runner. Missing prerequisites or
unavailable upstream services fail these jobs. `release-check` also requires
the fixture profile. The PHP source build is an explicit separate profile;
it is not silently included in the network smoke or default Go tests.

## Function × state matrix

| Function | Required registered scenarios / states | Proof |
|---|---|---|
| PHP CLI lifecycle | `php-cli-retry`: resolve/download/checksum/extract/configure/make/install failures → no publication → retry in the same root → current/which/ls → clear cache → protected and forced uninstall | Fixture; roots contain spaces |
| PHP transaction | `php-rollback`, `php-publication`: configure/make/partial install/runtime/version/ABI/startup/metadata/ini failure; prior bytes and modes survive; success preserves user files and publishes ready metadata | Fixture |
| PHP cancellation | `cli-signal-retry`: SIGINT/SIGTERM, child process termination, cleanup and successful retry | Fixture, POSIX |
| Interrupted/corrupt PHP state | `interrupted-recovery`, `installed-corruption`: saved interrupted journals, recovery, staging/failed/wrong-version/corrupt metadata excluded from installed versions | Fixture; portable registry entries |
| Shared state | `cli-shared-lock`: separate CLI processes serialize install, removal, Composer, extensions and cache mutation | Fixture, POSIX |
| Downloads | `download-concurrency`, `download-failed-refresh`, `download-bad-cache`: waiting writers recheck cache; truncation/cancellation preserves prior bytes; corrupt cache gets bounded replacement and cleanup | Fixture; portable registry entries |
| Signature policy | `gpg-states`: valid/invalid/unknown/revoked signer, missing status/signature/tool, strict and permitted fallback | Executable GPG fixture; not cryptographic upstream signing proof |
| Private dependencies | `dependency-trust`: version/source/hash/recipe identity and corrupt marker/cache trigger rebuild | Fixture |
| Extension publication/removal | `extension-publication`, `extension-stage-failure`: recorded ownership, exact published artifact; missing/ambiguous/wrong/unloadable artifact rejected before publication; owned artifact removed | Fixture |
| Extension cancellation/concurrency | `extension-cancel`, `extension-concurrency`: cancelled probe publishes nothing, concurrent metadata updates both retained | Fixture |
| PECL trust failure | `pecl-trust-failure`: metadata/size/checksum/bytes/truncation failures, bad cache replacement, no unverified phpize execution | Fixture |
| Composer compatibility | `composer-per-php`, `composer-pinned`, `composer-migration`: old/new PHP choose compatible releases, exact pin and rejection preserve the working PHAR, shared storage migrates without coupling future installs | Fixture |
| Ini snapshots | `ini-snapshot`, `ini-rollback`: save/use/resave, obsolete files removed, late validation failure restores previous configuration | Fixture |
| CLI automation | `cli-error-streams`: missing/unsafe binary, invalid arguments and network errors produce exit 1 and diagnostics on stderr | Fixture; portable registry entry |
| CLI colors | `cli-color-report`: forced/disabled report styles preserve the exact plain text; wider Go tests cover stream/environment policy, lifecycle states, machine output and independent loggers | Fixture, POSIX CLI; portable policy tests |
| Build diagnostics | `build-failure-diagnostics`, `configure-compiler-cause`: repeated linker errors stay in a redacted full log; CLI selects one bounded cause, including compiler detail from `config.log` | Fixture, POSIX |
| Build resources | `build-memory-jobs`, `php-space-extraction`, `php-space-install`, `extension-space`, `php-reinstall-space`: automatic jobs respond to available memory, explicit jobs win, low disk space stops extraction/publication, and forced reinstall counts old bytes | Fixture; PHP/PECL build cases POSIX |
| Composer update | `composer-real-php-update`, `composer-failures`, `composer-cancel`, `composer-concurrency`: real PHAR execution, per-PHP launchers, HTTP/checksum/runtime/version failure, cancellation, stale concurrent update cannot downgrade one PHP | Real PHP, synthetic PHAR |
| Ini runtime | `ini-real-php`: snapshot applied to real startup, invalid syntax/module rejected with rollback | Real PHP |
| Extension ABI | `extension-real-abi`: compiled native C fixture, real load, wrong ABI/name/dependency rejection and uninstall | Real PHP SDK on Linux |
| Upstream lifecycle | `upstream-tools-lifecycle`: official Composer install/update; PECL redis configure/make/load; disable → profile restore → cache clear → uninstall; ownership/trust metadata | Real PHP SDK, actual Composer/PECL |
| Official PECL channel | `upstream-pecl-channel`: official release metadata, archive identity and published file checksums | Real network, no compilation in this group |
| PHP source publication | `php-source-build`: real configure/make/INSTALL_ROOT, runtime/SDK/API validation and ready publication | Real source build, separate opt-in |

## Wider permanent regressions

`make test` runs the wider suite in addition to the selected scenario groups.
It includes version/alias/path validation, installed version resolution, effective
configuration, doctor, configure precedence, exact extension identity, Composer
cache migration and ini metadata. Representative files are:

- `internal/core/security_test.go`, `resolve_test.go` and `version_test.go`;
- `internal/cli/config_test.go`, `config_install_test.go`, `doctor_test.go`, `configure_flags_test.go`, `ext_identity_test.go` and `cache_composer_test.go`;
- `internal/ext/identity_test.go`, `manifest_test.go` and `security_test.go`;
- `internal/remote/download_checks_test.go`, `client_test.go`, `verify_test.go` and `pecl_trust_test.go`;
- `internal/ini/profile_test.go` and `profile_security_test.go`.

`make test-release`, `make test-installers` and `make test-checks` cover isolated
Git release failure/retry, checksum-bound installer publication, archive
validation, security baseline and scenario evidence accounting. These checks
remain part of release preflight. They use temporary local repositories and
controlled downloads; packaging smoke executes the host binary and validates
foreign executable metadata. Compilation or metadata inspection of a foreign
binary is not runtime execution on that OS.

## Platform and evidence limits

Linux is the mandatory scenario runner platform. The registry lists portable
and POSIX fixtures that can run on other platforms, but this does not certify
every lifecycle on those systems. Existing CI also runs the general Go suite
and native release archive smoke on Linux, macOS and Windows; their future CI
results are separate evidence. PHP source installation on Windows and native
Windows Composer/PECL lifecycles are not covered by the Linux SDK smoke.

The upstream scenario uses temporary launchers pointing to a read-only existing
PHP SDK, and publishes libraries/configuration only under its temporary root.
It does not build PHP itself. It installs the current stable Composer before
calling update, so this smoke also permits an already-current update; actual
replacement and stale-update rejection use controlled PHAR fixtures.

The native root has no spaces: the tested PECL redis configure script does not
accept the PHP SDK prefix with spaces. The CLI fixture deliberately tests its
own argument and path handling with spaces. Passing that fixture does not prove
that arbitrary upstream build scripts accept such prefixes.

Recovery uses saved interrupted journals rather than a power-loss test. A
runtime-fixture or upstream success does not prove every failure state against
real upstream software. Tests that require unavailable tools or permission
conditions may skip in the general Go suite; required profiles reject such
skips. Shell reinitialization, Windows PHP source backend and failure cleanup
of generic atomic file operations need their own implementation and regression
work; this matrix does not certify them.

## Isolated PHP source build

Supply an archive and an independently trusted digest for the intended version:

```bash
PHVM_NATIVE_PHP_ARCHIVE=/path/to/php-8.3.30.tar.xz \
PHVM_NATIVE_PHP_VERSION=8.3.30 \
PHVM_NATIVE_PHP_SHA256=YOUR_TRUSTED_SHA256 \
PHVM_NATIVE_PHP_LOG=/tmp/phvm-native-build.log \
make test-native-build
```

The test requires an exact nonempty SHA256 match. It does not acquire or verify
a GPG signature for the supplied archive; checksum/source authenticity must be
established before invoking this profile. Computing the digest of the same
untrusted file alone does not authenticate its origin.
