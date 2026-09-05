package launcher

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// lunarAgentJar is the javaagent that patches Lunar's canPlayOnline() so the
// fake JWT in accounts.json is never validated (mirror of lunar.py).
func lunarAgentJar() string {
	return filepath.Join(lunarBaseDir(), "agent", "lunar-agent.jar")
}

func lunarFindJavac() string {
	return findJavacIn(javaDir())
}

// findJavacIn scans a bundled-java root for the newest javac (JDK 17+,
// --release 17 needs at least that) so any already-downloaded JDK works.
func findJavacIn(javaRoot string) string {
	name := "javac"
	if runtime.GOOS == "windows" {
		name = "javac.exe"
	}
	var majors []int
	entries, err := os.ReadDir(javaRoot)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var m int
		if _, err := fmt.Sscanf(e.Name(), "jdk%d", &m); err == nil && m >= 17 {
			majors = append(majors, m)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(majors)))
	for _, m := range majors {
		p := filepath.Join(javaRoot, fmt.Sprintf("jdk%d", m), "bin", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// lunarAgentSource is the bytecode transformer from lunar.py, verbatim.
const lunarAgentSource = `import java.lang.instrument.ClassFileTransformer;
import java.lang.instrument.Instrumentation;
import java.security.ProtectionDomain;
import java.nio.charset.StandardCharsets;
import java.io.PrintStream;

public class Agent implements ClassFileTransformer {
    static PrintStream log = System.out;

    public static void premain(String args, Instrumentation inst) {
        inst.addTransformer(new Agent());
        log.println("[Agent] premain() OK");
    }

    public byte[] transform(ClassLoader l, String cn, Class<?> d,
            ProtectionDomain pd, byte[] b) {
        if (cn == null || b == null) return null;
        String body = new String(b, StandardCharsets.ISO_8859_1);

        // Search classes containing these strings
        if (!body.contains("canPlayOnline") &&
            !body.contains("launcher_accounts.json") &&
            !body.contains("accounts.json") &&
            !cn.contains("Account"))
            return null;

        log.println("[Agent] scanning: " + cn);
        byte[] patched = patch(b);
        if (patched != b) {
            log.println("[Agent] PATCHED: " + cn);
        }
        return patched;
    }

    // Opcode length table (JVM spec 6.)
    private static final int[] OP_LEN = new int[256];
    static {
        for (int i = 0; i < 256; i++) OP_LEN[i] = 1;
        // 2-byte
        for (int i = 0x10; i <= 0x11; i++) OP_LEN[i] = 2; // bipush, sipush
        OP_LEN[0x12] = 2; // ldc
        for (int i = 0x15; i <= 0x19; i++) OP_LEN[i] = 2; // *load
        for (int i = 0x36; i <= 0x3A; i++) OP_LEN[i] = 2; // *store
        OP_LEN[0xA9] = 2; // ret
        OP_LEN[0xBC] = 2; // newarray
        // 3-byte
        for (int i = 0x13; i <= 0x14; i++) OP_LEN[i] = 3; // ldc_w, ldc2_w
        for (int i = 0x99; i <= 0xA8; i++) OP_LEN[i] = 3; // if* + goto + jsr
        for (int i = 0xB2; i <= 0xB8; i++) OP_LEN[i] = 3; // getstatic..invokestatic
        OP_LEN[0xBB] = 3; // new
        OP_LEN[0xBD] = 3; // anewarray
        OP_LEN[0xC0] = 3; // checkcast
        OP_LEN[0xC1] = 3; // instanceof
        OP_LEN[0xC6] = 3; // ifnull
        OP_LEN[0xC7] = 3; // ifnonnull
        OP_LEN[0x84] = 3; // iinc
        OP_LEN[0xDA] = 3; // retry (jsr_w = 5 but handled below)
        // 4-byte
        OP_LEN[0xC5] = 4; // multianewarray
        // 5-byte
        OP_LEN[0xB9] = 5; // invokeinterface
        OP_LEN[0xBA] = 5; // invokedynamic
        OP_LEN[0xC8] = 5; // goto_w
        OP_LEN[0xC9] = 5; // jsr_w
        // tableswitch (0xAA) and lookupswitch (0xAB) are variable;
        // We'll just skip forward by 1 for them (won't miss ldc this way)
    }

    /** Return true if the Code attribute of this method contains ldc referencing a string with the given substring. */
    private static boolean methodRefsString(byte[] cls, int methodStart, String c, String search, boolean[] isAccountsCp) {
        int attrCount = readU2(cls, methodStart + 6);
        int attrIdx = methodStart + 8;
        for (int i = 0; i < attrCount; i++) {
            int nameIdx = readU2(cls, attrIdx);
            int len = readU4(cls, attrIdx + 2);
            if ("Code".equals(utf8(cls, c, nameIdx))) {
                int codeLen = readU4(cls, attrIdx + 10);
                int codeStart = attrIdx + 14;
                for (int pc = 0; pc < codeLen; ) {
                    int op = cls[codeStart + pc] & 0xFF;
                    if (op == 0x12) { // ldc
                        int cpIdx = cls[codeStart + pc + 1] & 0xFF;
                        if (cpIdx < isAccountsCp.length && isAccountsCp[cpIdx]) return true;
                        pc += 2;
                    } else if (op == 0x13) { // ldc_w
                        int cpIdx = readU2(cls, codeStart + pc + 1);
                        if (cpIdx < isAccountsCp.length && isAccountsCp[cpIdx]) return true;
                        pc += 3;
                    } else if (op == 0xAA || op == 0xAB) {
                        // tableswitch/lookupswitch - skip to next aligned boundary + parse
                        // Conservative: skip 1 and let the loop continue (won't cause false positives)
                        pc++;
                    } else {
                        int adv = OP_LEN[op];
                        pc += adv;
                    }
                }
                return false;
            }
            attrIdx += 6 + len;
        }
        return false;
    }

    private static byte[] patch(byte[] cls) {
        try {
            int cp = readU2(cls, 8), idx = 10;
            // First pass: parse CP and find which indices hold strings containing "Accounts"
            boolean[] isAccountsCp = new boolean[cp + 1];
            int stackMapTableCp = -1;
            for (int i = 1; i < cp; i++) {
                int t = cls[idx++] & 0xFF;
                switch (t) {
                    case 1: {
                        int l = readU2(cls, idx);
                        String s = new String(cls, idx + 2, l, StandardCharsets.UTF_8);
                        if (s.contains("Accounts") || s.contains("accounts")) isAccountsCp[i] = true;
                        if ("StackMapTable".equals(s)) stackMapTableCp = i;
                        idx += 2 + l; break;
                    }
                    case 3: case 4: idx += 4; break;
                    case 5: case 6: idx += 8; i++; break;
                    case 7: case 8: case 16: case 19: case 20: idx += 2; break;
                    case 9: case 10: case 11: case 17: case 18: idx += 4; break;
                    case 12: idx += 4; break;
                    case 15: idx += 3; break;
                    default: return cls;
                }
            }
            String c = new String(cls, StandardCharsets.ISO_8859_1);
            idx += 6; int ic = readU2(cls, idx); idx += 2 + ic * 2;
            int fc = readU2(cls, idx); idx += 2;
            for (int i = 0; i < fc; i++) { idx += 6; int a = readU2(cls, idx); idx += 2; for (int j = 0; j < a; j++) { idx += 2; int al = readU4(cls, idx); idx += 4 + al; } }
            int mc = readU2(cls, idx); idx += 2;
            // Collect ALL ()Z method indices
            int[] zMethods = new int[mc]; // one slot per method
            String[] zNames = new String[mc];
            int zc = 0;
            for (int i = 0; i < mc; i++) {
                String n = utf8(cls, c, readU2(cls, idx + 2));
                String d2 = utf8(cls, c, readU2(cls, idx + 4));
                if (!"<init>".equals(n) && !"<clinit>".equals(n) && "()Z".equals(d2)) {
                    zMethods[zc] = i; zNames[zc] = n; zc++;
                    log.println("[Agent]   matched: " + n + "()Z");
                }
                idx += 6; int a = readU2(cls, idx); idx += 2;
                for (int j = 0; j < a; j++) { idx += 2; int al = readU4(cls, idx); idx += 4 + al; }
            }
            if (zc == 0) return cls;
            log.println("[Agent]   patching " + zc + " boolean methods...");

            // Third pass: patch all collected methods' Code attribute with "return true"
            idx = 10;
            for (int i = 1; i < cp; i++) {
                int t = cls[idx++] & 0xFF;
                switch (t) {
                    case 1: { int l = readU2(cls, idx); idx += 2 + l; break; }
                    case 3: case 4: idx += 4; break;
                    case 5: case 6: idx += 8; i++; break;
                    case 7: case 8: case 16: case 19: case 20: idx += 2; break;
                    case 9: case 10: case 11: case 17: case 18: idx += 4; break;
                    case 12: idx += 4; break;
                    case 15: idx += 3; break;
                    default: return cls;
                }
            }
            idx += 6; idx += 2 + ic * 2;
            idx += 2; for (int i = 0; i < fc; i++) { idx += 6; int a = readU2(cls, idx); idx += 2; for (int j = 0; j < a; j++) { idx += 2; int al = readU4(cls, idx); idx += 4 + al; } }
            int mc2 = readU2(cls, idx); idx += 2;
            byte[] p = cls.clone();
            boolean patched = false;
            for (int i = 0; i < mc2; i++) {
                int thisMethodIdx = i;
                idx += 6; int a = readU2(cls, idx); idx += 2;
                for (int j = 0; j < a; j++) {
                    int an = readU2(cls, idx); int al = readU4(cls, idx + 2);
                    // Check if this method is in our target list
                    boolean isTarget = false;
                    for (int k = 0; k < zc; k++) {
                        if (zMethods[k] == thisMethodIdx) { isTarget = true; break; }
                    }
                    if (isTarget && "Code".equals(utf8(cls, c, an)) && al >= 22 && stackMapTableCp > 0) {
                        log.println("[Agent]     -> method #" + thisMethodIdx + " return true");
                        // NOPs then iconst_1; ireturn — NOPs after the return
                        // would start a dead basic block and the verifier
                        // would demand a stack map frame there (JDK 13+
                        // ignores -noverify). The empty StackMapTable keeps
                        // the attribute valid; codeLen is chosen so the Code
                        // attribute exactly fills its declared length
                        // (al = 20 + codeLen).
                        int codeLen = al - 20;
                        java.util.Arrays.fill(p, idx + 14, idx + 14 + codeLen - 2, (byte)0);
                        p[idx + 14 + codeLen - 2] = 4; p[idx + 14 + codeLen - 1] = (byte)0xAC;
                        writeU4(p, idx + 10, codeLen);                  // code_length
                        writeU2(p, idx + 6, 1);                         // max_stack
                        writeU2(p, idx + 8, 1);                         // max_locals
                        writeU2(p, idx + 14 + codeLen, 0);              // exception_table_length = 0
                        writeU2(p, idx + 14 + codeLen + 2, 1);          // attributes_count = 1
                        writeU2(p, idx + 14 + codeLen + 4, stackMapTableCp); // StackMapTable name_index
                        writeU4(p, idx + 14 + codeLen + 6, 2);          // attribute_length = 2
                        writeU2(p, idx + 14 + codeLen + 10, 0);         // number_of_entries = 0
                        patched = true;
                    }
                    idx += 6 + al;
                }
            }
            if (patched) return p;
        } catch (Exception e) { e.printStackTrace(); }
        return cls;
    }
    private static String utf8(byte[] cls, String c, int idx) {
        int p = 10;
        for (int i = 1; i < idx; i++) {
            int t = cls[p++] & 0xFF;
            switch (t) {
                case 1: p += 2 + readU2(cls, p); break;
                case 3: case 4: p += 4; break;
                case 5: case 6: p += 8; i++; break;
                case 7: case 8: case 16: case 19: case 20: p += 2; break;
                case 9: case 10: case 11: case 17: case 18: p += 4; break;
                case 12: p += 4; break;
                case 15: p += 3; break;
                default: return "";
            }
        }
        if ((cls[p] & 0xFF) == 1) { int l = readU2(cls, p + 1); try { return new String(cls, p + 3, l, StandardCharsets.UTF_8); } catch (Exception e) {} }
        return "";
    }
    private static int readU2(byte[] b, int off) { return ((b[off] & 0xFF) << 8) | (b[off+1] & 0xFF); }
    private static int readU4(byte[] b, int off) { return ((b[off] & 0xFF) << 24) | ((b[off+1] & 0xFF) << 16) | ((b[off+2] & 0xFF) << 8) | (b[off+3] & 0xFF); }
    private static void writeU2(byte[] b, int off, int v) { b[off] = (byte)(v >> 8); b[off+1] = (byte)(v); }
    private static void writeU4(byte[] b, int off, int v) { b[off] = (byte)(v >> 24); b[off+1] = (byte)(v >> 16); b[off+2] = (byte)(v >> 8); b[off+3] = (byte)(v); }
}
`

// lunarEnsureAgent builds lunar-agent.jar with the bundled JDK if missing
// or stale (mirror of lunar.py _ensure_agent).
func lunarEnsureAgent(version string, onProgress ProgressFn) (string, error) {
	agentJar := lunarAgentJar()
	sum := sha256.Sum256([]byte(lunarAgentSource))
	marker := filepath.Join(lunarBaseDir(), "agent", ".source-sha256")
	if data, err := os.ReadFile(marker); err == nil && string(data) == hex.EncodeToString(sum[:]) {
		if _, err := os.Stat(agentJar); err == nil {
			return agentJar, nil
		}
	}
	javac := lunarFindJavac()
	if javac == "" {
		major, err := requiredJavaMajor(version)
		if err != nil {
			return "", fmt.Errorf("lunar agent: %w", err)
		}
		if _, err := EnsureJava(major, onProgress); err != nil {
			return "", fmt.Errorf("lunar agent: no bundled JDK: %w", err)
		}
		javac = lunarFindJavac()
		if javac == "" {
			return "", fmt.Errorf("lunar agent: javac not found after JDK install")
		}
	}
	dir := filepath.Join(lunarBaseDir(), "agent")
	os.MkdirAll(dir, 0o755)
	src := filepath.Join(dir, "Agent.java")
	if err := os.WriteFile(src, []byte(lunarAgentSource), 0o644); err != nil {
		return "", err
	}
	out, err := exec.Command(javac, "--release", "17", "-encoding", "utf-8", "-d", dir, src).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("lunar agent javac: %v\n%s", err, out)
	}
	mf := filepath.Join(dir, "MANIFEST.MF")
	if err := os.WriteFile(mf, []byte("Manifest-Version: 1.0\nPremain-Class: Agent\n\n"), 0o644); err != nil {
		return "", err
	}
	jarTool := filepath.Join(filepath.Dir(javac), "jar")
	if runtime.GOOS == "windows" {
		jarTool += ".exe"
	}
	out, err = exec.Command(jarTool, "cfm", agentJar, mf, "-C", dir, "Agent.class").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("lunar agent jar: %v\n%s", err, out)
	}
	for _, f := range []string{"Agent.java", "Agent.class", "MANIFEST.MF"} {
		os.Remove(filepath.Join(dir, f))
	}
	os.WriteFile(marker, []byte(hex.EncodeToString(sum[:])), 0o644)
	return agentJar, nil
}

// lunarGenerateFakeJWT builds a structurally valid JWT with a fake signature;
// the agent patches canPlayOnline() so Lunar never validates it (mirror of
// lunar.py _generate_fake_jwt).
func lunarGenerateFakeJWT(username, uuidStr string) string {
	b64 := func(b []byte) string {
		return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=")
	}
	header := b64([]byte(`{"alg":"RS256","kid":"offline"}`))
	now := time.Now().Unix()
	payload := b64([]byte(fmt.Sprintf(`{"sub":%q,"username":%q,"iat":%d,"exp":%d,"platform":"PC_LAUNCHER"}`,
		uuidStr, username, now, now+86400*30)))
	return header + "." + payload + "." + b64(make([]byte, 256))
}

// lunarEnsureFakeAccounts writes accounts.json with the current player so
// Lunar skips Microsoft auth via Electron IPC (mirror of lunar.py
// _ensure_fake_accounts).
func lunarEnsureFakeAccounts(username, uuidStr string) error {
	localID := strings.ReplaceAll(uuidStr, "-", "")
	if localID == "00000000000000000000000000000000" {
		localID = strings.ReplaceAll(uuid.NewMD5(uuid.NameSpaceDNS, []byte("OfflinePlayer:"+username)).String(), "-", "")
	}
	accounts := map[string]any{
		"activeAccountLocalId": localID,
		"accounts": map[string]any{
			localID: map[string]any{
				"accessToken": lunarGenerateFakeJWT(username, uuidStr),
				"username":    username,
				"localId":     localID,
				"minecraftProfile": map[string]string{
					"id":   uuidStr,
					"name": username,
				},
				"type":                 "mojang",
				"persistent":           true,
				"legacy":               false,
				"eligibleForMigration": false,
				"hasMultipleProfiles":  false,
				"accessTokenExpiresAt": "2099-01-01T00:00:00.000Z",
			},
		},
	}
	path := filepath.Join(lunarBaseDir(), "settings", "game", "accounts.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	data, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
