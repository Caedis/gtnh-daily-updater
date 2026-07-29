package versionstamp

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

var testVersion = DisplayVersion{
	Short: "2.9.x (Daily 648)",
	Long:  "2.9.x (Daily 648) - 2026-07-28",
	Date:  "2026-07-28",
}

// writeTestFile creates path and its parents with the given content.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fullInstance builds a client-style instance (instanceDir + .minecraft gameDir)
// carrying every target file with pack-default content.
func fullInstance(t *testing.T) (instanceDir, gameDir string) {
	t.Helper()
	instanceDir = t.TempDir()
	gameDir = filepath.Join(instanceDir, ".minecraft")

	writeTestFile(t, filepath.Join(gameDir, "config", "txloader", "load", "mainmenu", "version.txt"), "GTNH 2.9.0")
	writeTestFile(t, filepath.Join(gameDir, "config", "GTNewHorizons", "dreamcraft.cfg"),
		"# header\ngeneral {\n    S:ModPackVersion=2.9.0\n    B:Other=true\n}\n")
	writeTestFile(t, filepath.Join(gameDir, "config", "DreamCoreMod.properties"),
		"#Config file for the ASM part of GTNHCoreMod\ndownloadOnlyOnce=true\ndisplayedModpackVersion=2.9.0\n")
	writeTestFile(t, filepath.Join(instanceDir, "server.properties"),
		"#Minecraft server properties\nmotd=GT:New Horizons 2.9.0\nserver-port=25565\n")
	writeTestFile(t, filepath.Join(instanceDir, "instance.cfg"),
		"InstanceType=OneSix\nname=GTNH 2.9.0\niconKey=gtnh_icon\n")
	return instanceDir, gameDir
}

func TestApplyStampsAllTargets(t *testing.T) {
	instanceDir, gameDir := fullInstance(t)

	stamped, err := Apply(instanceDir, gameDir, testVersion)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	want := []string{
		"config/txloader/load/mainmenu/version.txt",
		"config/GTNewHorizons/dreamcraft.cfg",
		"config/DreamCoreMod.properties",
		"server.properties",
		"instance.cfg",
	}
	if !slices.Equal(stamped, want) {
		t.Fatalf("stamped = %v, want %v", stamped, want)
	}

	if got := readTestFile(t, filepath.Join(gameDir, "config", "txloader", "load", "mainmenu", "version.txt")); got != "GTNH 2.9.x (Daily 648) (2026-07-28)" {
		t.Errorf("version.txt = %q", got)
	}
	if got := readTestFile(t, filepath.Join(gameDir, "config", "GTNewHorizons", "dreamcraft.cfg")); got != "# header\ngeneral {\n    S:ModPackVersion=2.9.x (Daily 648) - 2026-07-28\n    B:Other=true\n}\n" {
		t.Errorf("dreamcraft.cfg = %q", got)
	}
	if got := readTestFile(t, filepath.Join(gameDir, "config", "DreamCoreMod.properties")); got != "#Config file for the ASM part of GTNHCoreMod\ndownloadOnlyOnce=true\ndisplayedModpackVersion=2.9.x (Daily 648) - 2026-07-28\n" {
		t.Errorf("DreamCoreMod.properties = %q", got)
	}
	if got := readTestFile(t, filepath.Join(instanceDir, "server.properties")); got != "#Minecraft server properties\nmotd=GT:New Horizons 2.9.x (Daily 648) - 2026-07-28\nserver-port=25565\n" {
		t.Errorf("server.properties = %q", got)
	}
	if got := readTestFile(t, filepath.Join(instanceDir, "instance.cfg")); got != "InstanceType=OneSix\nname=GTNH 2.9.x (Daily 648) - 2026-07-28\niconKey=gtnh_icon\n" {
		t.Errorf("instance.cfg = %q", got)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	instanceDir, gameDir := fullInstance(t)

	versionTxt := filepath.Join(gameDir, "config", "txloader", "load", "mainmenu", "version.txt")
	coreMod := filepath.Join(gameDir, "config", "DreamCoreMod.properties")

	if _, err := Apply(instanceDir, gameDir, testVersion); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	beforeServerProps := readTestFile(t, filepath.Join(instanceDir, "server.properties"))
	beforeVersionTxt := readTestFile(t, versionTxt)
	beforeCoreMod := readTestFile(t, coreMod)

	stamped, err := Apply(instanceDir, gameDir, testVersion)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if len(stamped) != 0 {
		t.Errorf("second Apply stamped %v, want nothing", stamped)
	}
	if after := readTestFile(t, filepath.Join(instanceDir, "server.properties")); after != beforeServerProps {
		t.Errorf("server.properties changed on second apply: %q -> %q", beforeServerProps, after)
	}
	if after := readTestFile(t, versionTxt); after != beforeVersionTxt {
		t.Errorf("version.txt changed on second apply: %q -> %q", beforeVersionTxt, after)
	}
	if after := readTestFile(t, coreMod); after != beforeCoreMod {
		t.Errorf("DreamCoreMod.properties changed on second apply: %q -> %q", beforeCoreMod, after)
	}
}

// TestApplyPreservesFileMode verifies stamping rewrites content without
// altering the file's existing permission bits.
func TestApplyPreservesFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits not meaningful on windows")
	}
	instanceDir, gameDir := fullInstance(t)
	path := filepath.Join(instanceDir, "server.properties")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Apply(instanceDir, gameDir, testVersion); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("server.properties mode = %o, want %o", got, 0o600)
	}
}

func TestApplySkipsMissingFiles(t *testing.T) {
	instanceDir := t.TempDir()
	gameDir := instanceDir // server layout, no .minecraft

	stamped, err := Apply(instanceDir, gameDir, testVersion)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(stamped) != 0 {
		t.Errorf("stamped = %v, want nothing", stamped)
	}
}

func TestApplySkipsDreamcraftWithoutKey(t *testing.T) {
	instanceDir := t.TempDir()
	gameDir := instanceDir
	path := filepath.Join(gameDir, "config", "GTNewHorizons", "dreamcraft.cfg")
	writeTestFile(t, path, "general {\n    B:Other=true\n}\n")

	stamped, err := Apply(instanceDir, gameDir, testVersion)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(stamped) != 0 {
		t.Errorf("stamped = %v, want nothing", stamped)
	}
	if got := readTestFile(t, path); got != "general {\n    B:Other=true\n}\n" {
		t.Errorf("dreamcraft.cfg = %q, want untouched", got)
	}
}

func TestApplyAppendsMissingCoreModKey(t *testing.T) {
	instanceDir := t.TempDir()
	gameDir := instanceDir
	path := filepath.Join(gameDir, "config", "DreamCoreMod.properties")
	writeTestFile(t, path, "downloadOnlyOnce=true\n")

	stamped, err := Apply(instanceDir, gameDir, testVersion)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !slices.Contains(stamped, "config/DreamCoreMod.properties") {
		t.Fatalf("stamped = %v, want DreamCoreMod.properties", stamped)
	}
	want := "downloadOnlyOnce=true\ndisplayedModpackVersion=2.9.x (Daily 648) - 2026-07-28\n"
	if got := readTestFile(t, path); got != want {
		t.Errorf("DreamCoreMod.properties = %q, want %q", got, want)
	}
}

func TestApplyLeavesCustomMotdAndName(t *testing.T) {
	instanceDir, gameDir := fullInstance(t)
	custom := "#Minecraft server properties\nmotd=Bob's Server\nserver-port=25565\n"
	writeTestFile(t, filepath.Join(instanceDir, "server.properties"), custom)
	customCfg := "InstanceType=OneSix\nname=My Pack\niconKey=gtnh_icon\n"
	writeTestFile(t, filepath.Join(instanceDir, "instance.cfg"), customCfg)

	stamped, err := Apply(instanceDir, gameDir, testVersion)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if slices.Contains(stamped, "server.properties") || slices.Contains(stamped, "instance.cfg") {
		t.Fatalf("stamped = %v, want neither server.properties nor instance.cfg", stamped)
	}
	if got := readTestFile(t, filepath.Join(instanceDir, "server.properties")); got != custom {
		t.Errorf("server.properties = %q, want untouched", got)
	}
	if got := readTestFile(t, filepath.Join(instanceDir, "instance.cfg")); got != customCfg {
		t.Errorf("instance.cfg = %q, want untouched", got)
	}
}

// TestApplyGuardNearMisses proves prefixGuard is stricter than a plain
// strings.HasPrefix: values that merely start with the pack prefix but aren't
// "prefix" or "prefix " must still be treated as customized.
func TestApplyGuardNearMisses(t *testing.T) {
	cases := []struct {
		name  string
		motd  string
		iname string
	}{
		{"concatenated-suffix", "GT:New Horizonsable", "GTNH-custom"},
		{"empty-value", "", ""},
		{"wrong-case", "gt:new horizons 2.9.0", "gtnhable"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			instanceDir, gameDir := fullInstance(t)
			serverProps := "#Minecraft server properties\nmotd=" + c.motd + "\nserver-port=25565\n"
			writeTestFile(t, filepath.Join(instanceDir, "server.properties"), serverProps)
			instanceCfgContent := "InstanceType=OneSix\nname=" + c.iname + "\niconKey=gtnh_icon\n"
			writeTestFile(t, filepath.Join(instanceDir, "instance.cfg"), instanceCfgContent)

			stamped, err := Apply(instanceDir, gameDir, testVersion)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if slices.Contains(stamped, "server.properties") || slices.Contains(stamped, "instance.cfg") {
				t.Fatalf("stamped = %v, want neither server.properties nor instance.cfg", stamped)
			}
			if got := readTestFile(t, filepath.Join(instanceDir, "server.properties")); got != serverProps {
				t.Errorf("server.properties = %q, want untouched", got)
			}
			if got := readTestFile(t, filepath.Join(instanceDir, "instance.cfg")); got != instanceCfgContent {
				t.Errorf("instance.cfg = %q, want untouched", got)
			}
		})
	}
}

// TestApplyStampsBarePackDefault proves the guard still accepts the exact
// bare pack default (no trailing version), the "current == prefix" branch.
func TestApplyStampsBarePackDefault(t *testing.T) {
	instanceDir, gameDir := fullInstance(t)
	writeTestFile(t, filepath.Join(instanceDir, "server.properties"),
		"#Minecraft server properties\nmotd=GT:New Horizons\nserver-port=25565\n")
	writeTestFile(t, filepath.Join(instanceDir, "instance.cfg"),
		"InstanceType=OneSix\nname=GTNH\niconKey=gtnh_icon\n")

	stamped, err := Apply(instanceDir, gameDir, testVersion)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !slices.Contains(stamped, "server.properties") || !slices.Contains(stamped, "instance.cfg") {
		t.Fatalf("stamped = %v, want both server.properties and instance.cfg", stamped)
	}
	want := "#Minecraft server properties\nmotd=GT:New Horizons " + testVersion.Long + "\nserver-port=25565\n"
	if got := readTestFile(t, filepath.Join(instanceDir, "server.properties")); got != want {
		t.Errorf("server.properties = %q, want %q", got, want)
	}
	wantCfg := "InstanceType=OneSix\nname=GTNH " + testVersion.Long + "\niconKey=gtnh_icon\n"
	if got := readTestFile(t, filepath.Join(instanceDir, "instance.cfg")); got != wantCfg {
		t.Errorf("instance.cfg = %q, want %q", got, wantCfg)
	}
}

func TestApplyNonDevVersionOmitsDateFromMainMenu(t *testing.T) {
	instanceDir, gameDir := fullInstance(t)
	v := DisplayVersion{Short: "2.9.0", Long: "2.9.0"}

	if _, err := Apply(instanceDir, gameDir, v); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := readTestFile(t, filepath.Join(gameDir, "config", "txloader", "load", "mainmenu", "version.txt")); got != "GTNH 2.9.0" {
		t.Errorf("version.txt = %q, want %q", got, "GTNH 2.9.0")
	}
}

// TestApplyContinuesPastOneTargetFailure verifies that a read error on one
// target doesn't stop the rest, and that the error surfaces mentioning the
// failing path.
func TestApplyContinuesPastOneTargetFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 0000 does not block reads")
	}
	instanceDir, gameDir := fullInstance(t)
	dreamcraft := filepath.Join(gameDir, "config", "GTNewHorizons", "dreamcraft.cfg")
	if err := os.Chmod(dreamcraft, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dreamcraft, 0o644)

	stamped, err := Apply(instanceDir, gameDir, testVersion)
	if err == nil {
		t.Fatal("Apply: want error, got nil")
	}
	if !strings.Contains(err.Error(), dreamcraft) {
		t.Errorf("Apply error = %q, want it to mention %q", err.Error(), dreamcraft)
	}

	for _, name := range []string{
		"config/txloader/load/mainmenu/version.txt",
		"config/DreamCoreMod.properties",
		"server.properties",
		"instance.cfg",
	} {
		if !slices.Contains(stamped, name) {
			t.Errorf("stamped = %v, want it to include %q despite the other failure", stamped, name)
		}
	}
}

// TestApplyStripsTrailingNewlineFromMainMenu proves version.txt is rewritten
// without a trailing newline even when the source file has one.
func TestApplyStripsTrailingNewlineFromMainMenu(t *testing.T) {
	instanceDir := t.TempDir()
	gameDir := instanceDir
	path := filepath.Join(gameDir, "config", "txloader", "load", "mainmenu", "version.txt")
	writeTestFile(t, path, "GTNH 2.9.0\n")

	if _, err := Apply(instanceDir, gameDir, testVersion); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := "GTNH " + testVersion.Short + " (" + testVersion.Date + ")"
	if got := readTestFile(t, path); got != want {
		t.Errorf("version.txt = %q, want %q (no trailing newline)", got, want)
	}
}

func TestApplyPreservesCRLFServerProperties(t *testing.T) {
	instanceDir := t.TempDir()
	gameDir := instanceDir
	path := filepath.Join(instanceDir, "server.properties")
	writeTestFile(t, path, "motd=GT:New Horizons 2.9.0\r\nserver-port=25565\r\n")

	if _, err := Apply(instanceDir, gameDir, testVersion); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := "motd=GT:New Horizons 2.9.x (Daily 648) - 2026-07-28\r\nserver-port=25565\r\n"
	if got := readTestFile(t, path); got != want {
		t.Errorf("server.properties = %q, want %q", got, want)
	}
}
