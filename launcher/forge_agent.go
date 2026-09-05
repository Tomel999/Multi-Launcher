package launcher

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// fmlSideAgentSource is a javaagent that presets
// net.minecraftforge.fml.relauncher.FMLRelaunchLog.side to CLIENT before the
// game starts. Legacy Forge (1.8.9) only sets that field once its own tweaker
// runs, but Mixin's legacy FML agent logs through FMLRelaunchLog during
// MixinTweaker construction — earlier — and dies with an NPE in
// FMLRelaunchLog.configureLogging. The agent runs before main, so the field
// is already set when Mixin gets there. Only attached to launchwrapper
// (pre-1.13 Forge) launches; everything else never loads those classes and
// the agent stays inert.
const fmlSideAgentSource = `public class FmlSideAgent {
    public static void premain(String args, java.lang.instrument.Instrumentation inst) {
        try {
            Class<?> side = Class.forName("net.minecraftforge.fml.relauncher.Side");
            Object client = null;
            Object[] constants = (Object[])side.getMethod("values").invoke(null);
            for (int i = 0; i < constants.length; i++) {
                if ("CLIENT".equals(constants[i].toString())) client = constants[i];
            }
            if (client == null) return;
            Class<?> log = Class.forName("net.minecraftforge.fml.relauncher.FMLRelaunchLog");
            java.lang.reflect.Field f = log.getDeclaredField("side");
            f.setAccessible(true);
            if (f.get(null) == null) {
                f.set(null, client);
                System.out.println("[FmlSide] FMLRelaunchLog.side preset to CLIENT");
            }
        } catch (Throwable t) {
            System.out.println("[FmlSide] inactive (" + t + ")");
        }
    }
}
`

func fmlSideAgentJar() string {
	return filepath.Join(root(), "agent", "fml-side-agent.jar")
}

func ensureFmlSideAgent(onProgress ProgressFn) (string, error) {
	agentJar := fmlSideAgentJar()
	sum := sha256.Sum256([]byte(fmlSideAgentSource))
	marker := filepath.Join(root(), "agent", ".fml-side-sha256")
	if data, err := os.ReadFile(marker); err == nil && string(data) == hex.EncodeToString(sum[:]) {
		if _, err := os.Stat(agentJar); err == nil {
			return agentJar, nil
		}
	}
	javac := lunarFindJavac()
	if javac == "" {
		return "", fmt.Errorf("fml side agent: no bundled JDK with javac (need any modern JDK installed once)")
	}
	dir := filepath.Join(root(), "agent")
	os.MkdirAll(dir, 0o755)
	src := filepath.Join(dir, "FmlSideAgent.java")
	if err := os.WriteFile(src, []byte(fmlSideAgentSource), 0o644); err != nil {
		return "", err
	}
	out, err := exec.Command(javac, "--release", "8", "-encoding", "utf-8", "-d", dir, src).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("fml side agent javac: %v\n%s", err, out)
	}
	mf := filepath.Join(dir, "MANIFEST.MF")
	if err := os.WriteFile(mf, []byte("Manifest-Version: 1.0\nPremain-Class: FmlSideAgent\n\n"), 0o644); err != nil {
		return "", err
	}
	jarTool := filepath.Join(filepath.Dir(javac), "jar")
	if runtime.GOOS == "windows" {
		jarTool += ".exe"
	}
	out, err = exec.Command(jarTool, "cfm", agentJar, mf, "-C", dir, "FmlSideAgent.class").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("fml side agent jar: %v\n%s", err, out)
	}
	for _, f := range []string{"FmlSideAgent.java", "FmlSideAgent.class", "MANIFEST.MF"} {
		os.Remove(filepath.Join(dir, f))
	}
	os.WriteFile(marker, []byte(hex.EncodeToString(sum[:])), 0o644)
	return agentJar, nil
}
