package launcher

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"multilauncherwails/launcher/plugin"
)

type World struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Icon        string `json:"icon"`
	LastPlayed  int64  `json:"lastPlayed"`
	PlayTime    int64  `json:"playTime"`
	Size        int64  `json:"size"`
}

func savesDir(instName string) string {
	p := filepath.Join(InstanceDir(instName), "saves")
	os.MkdirAll(p, 0o755)
	return p
}

func ListWorlds(instName string) []World {
	dir := savesDir(instName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []World{}
	}
	out := []World{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ld := filepath.Join(dir, e.Name(), "level.dat")
		if _, err := os.Stat(ld); err != nil {
			continue
		}
		w := World{Name: e.Name()}
		if root, ok := parseLevelDat(ld); ok {
			data, _ := root.get("Data")
			w.DisplayName = strOf(data.get("LevelName"))
			w.Icon = strOf(data.get("icon"))
			w.LastPlayed = numOf(data.get("LastPlayed"))
			w.PlayTime = numOf(data.get("PlayTime"))
			if w.PlayTime == 0 {
				if uuid := uuidFromIntArray(data.get("singleplayer_uuid")); uuid != "" {
					w.PlayTime = statsPlayTime(filepath.Join(dir, e.Name()), uuid)
				}
			}
		}
		w.Size = dirSize(filepath.Join(dir, e.Name()))
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastPlayed > out[j].LastPlayed })
	return out
}

func DuplicateWorld(instName, name string) (string, error) {
	saves := savesDir(instName)
	newName := name + " (copy)"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(saves, newName)); os.IsNotExist(err) {
			break
		}
		newName = fmt.Sprintf("%s (copy %d)", name, i)
	}
	if err := copyDirRecursive(filepath.Join(saves, name), filepath.Join(saves, newName)); err != nil {
		return "", err
	}
	return newName, nil
}

func RenameWorld(instName, oldName, newName string) error {
	if strings.TrimSpace(newName) == "" {
		return fmt.Errorf("world name cannot be empty")
	}
	newName = sanitize(newName)
	old := filepath.Join(savesDir(instName), oldName)
	neu := filepath.Join(savesDir(instName), newName)
	if _, err := os.Stat(neu); err == nil {
		return fmt.Errorf("world %q already exists", newName)
	}
	return os.Rename(old, neu)
}

func DeleteWorld(instName, name string) error {
	return os.RemoveAll(filepath.Join(savesDir(instName), name))
}

func ExportWorld(instName, name, dst string) error {
	err := ZipDir(filepath.Join(savesDir(instName), name), dst)
	if err == nil {
		plugin.Bus.Emit("world.exported", plugin.WorldEvent{Instance: instName, World: name})
	}
	return err
}

func ImportWorld(instName, zipPath string) (string, error) {
	tmp, err := os.MkdirTemp("", "world-import-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err := unzipAll(zipPath, tmp); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return "", err
	}
	worldDir := ""
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(tmp, e.Name(), "level.dat")); err == nil {
			worldDir = e.Name()
			break
		}
	}
	if worldDir == "" {
		base := strings.TrimSuffix(filepath.Base(zipPath), filepath.Ext(zipPath))
		worldDir = sanitize(base)
		if worldDir == "" || worldDir == "instance" {
			worldDir = "imported-world"
		}
		if err := os.MkdirAll(filepath.Join(tmp, worldDir), 0o755); err != nil {
			return "", err
		}
		for _, e := range entries {
			if err := os.Rename(filepath.Join(tmp, e.Name()), filepath.Join(tmp, worldDir, e.Name())); err != nil {
				return "", err
			}
		}
	}
	dest := filepath.Join(savesDir(instName), worldDir)
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("world %q already exists", worldDir)
	}
	if err := os.Rename(filepath.Join(tmp, worldDir), dest); err != nil {
		return "", err
	}
	plugin.Bus.Emit("world.imported", plugin.WorldEvent{Instance: instName, World: worldDir})
	return worldDir, nil
}

func dirSize(dir string) int64 {
	var total int64
	filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func ZipDir(src, dst string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	defer zw.Close()
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, in)
		in.Close()
		return err
	})
}

type Item struct {
	ID           string   `json:"id"`
	Count        int      `json:"count"`
	Slot         int      `json:"slot"`
	Damage       int      `json:"damage"`
	Enchantments []string `json:"enchantments"`
}

type WorldInfo struct {
	Name        string     `json:"name"`
	DisplayName string     `json:"displayName"`
	Icon        string     `json:"icon"`
	LastPlayed  int64      `json:"lastPlayed"`
	PlayTime    int64      `json:"playTime"`
	Size        int64      `json:"size"`
	Seed        int64      `json:"seed"`
	Version     string     `json:"version"`
	Difficulty  int        `json:"difficulty"`
	Hardcore    bool       `json:"hardcore"`
	GameMode    int        `json:"gameMode"`
	Days        int64      `json:"days"`
	Pos         [3]float64 `json:"pos"`
	Dimension   string     `json:"dimension"`
	Health      float64    `json:"health"`
	FoodLevel   int        `json:"foodLevel"`
	XpLevel     int        `json:"xpLevel"`
	Deaths      int64      `json:"deaths"`
	MobKills    int64      `json:"mobKills"`
	PlayTimeSec int64      `json:"playTimeSec"`
	Inventory   []Item     `json:"inventory"`
	PlayerUUID  string     `json:"playerUuid"`
}

func GetWorldInfo(instName, worldName string) WorldInfo {
	dir := filepath.Join(savesDir(instName), worldName)
	w := WorldInfo{Name: worldName}
	root, ok := parseLevelDat(filepath.Join(dir, "level.dat"))
	if !ok {
		w.Size = dirSize(dir)
		return w
	}
	data, _ := root.get("Data")
	w.DisplayName = strOf(data.get("LevelName"))
	w.Icon = strOf(data.get("icon"))
	w.LastPlayed = numOf(data.get("LastPlayed"))
	w.PlayTime = numOf(data.get("PlayTime"))
	w.Seed = numOf(data.get("RandomSeed"))
	if w.Seed == 0 {
		w.Seed = numOf(data.get("WorldGenSettings", "seed"))
	}
	w.Days = numOf(data.get("Time")) / 24000
	if v, ok := data.get("Version", "Name"); ok {
		w.Version = v.str
	}
	w.GameMode = int(numOf(data.get("GameType")))
	if d, ok := data.get("difficulty_settings", "difficulty"); ok {
		w.Difficulty = difficultyValue(d, true)
	} else if d, ok := data.get("Difficulty"); ok {
		w.Difficulty = difficultyValue(d, true)
	}
	w.Hardcore = numOf(data.get("difficulty_settings", "hardcore")) != 0
	if player, ok := data.get("Player"); ok {
		extractPlayer(&w, player)
	}
	if stats, ok := data.get("Statistics"); ok {
		extractStats(&w, stats)
	}
	if uuid := uuidFromIntArray(data.get("singleplayer_uuid")); uuid != "" {
		w.PlayerUUID = uuid
		if pd, ok := parseLevelDat(filepath.Join(dir, "players", "data", uuid+".dat")); ok {
			extractPlayer(&w, pd)
		} else if pd, ok := parseLevelDat(filepath.Join(dir, "playerdata", uuid+".dat")); ok {
			extractPlayer(&w, pd)
		}
		if raw, err := os.ReadFile(filepath.Join(dir, "players", "stats", uuid+".json")); err == nil {
			extractStatsJSON(&w, raw)
		} else if raw, err := os.ReadFile(filepath.Join(dir, "stats", uuid+".json")); err == nil {
			extractStatsJSON(&w, raw)
		}
	}
	w.Size = dirSize(dir)
	return w
}

func difficultyValue(v nbtValue, _ bool) int {
	if v.typ == 8 {
		switch v.str {
		case "peaceful":
			return 0
		case "easy":
			return 1
		case "normal":
			return 2
		case "hard":
			return 3
		}
		return 0
	}
	return int(v.num)
}

func uuidFromIntArray(v nbtValue, _ bool) string {
	if v.typ != 11 || len(v.raw) != 16 {
		return ""
	}
	b := v.raw
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func extractPlayer(w *WorldInfo, player nbtValue) {
	w.Inventory = nil
	w.GameMode = int(numOf(player.get("playerGameType")))
	w.Dimension = strOf(player.get("Dimension"))
	w.Health = fltOf(player.get("Health"))
	w.FoodLevel = int(numOf(player.get("foodLevel")))
	w.XpLevel = int(numOf(player.get("XpLevel")))
	if pos, ok := player.get("Pos"); ok && len(pos.list) >= 3 {
		w.Pos = [3]float64{pos.list[0].flt, pos.list[1].flt, pos.list[2].flt}
	}
	if inv, ok := player.get("Inventory"); ok {
		for _, item := range inv.list {
			w.Inventory = append(w.Inventory, parseItem(item))
		}
	}
	if eq, ok := player.get("equipment"); ok {
		for _, e := range []struct {
			key  string
			slot int
		}{{"head", 103}, {"chest", 102}, {"legs", 101}, {"feet", 100}, {"offhand", 150}} {
			if item, ok := eq.get(e.key); ok {
				it := parseItem(item)
				it.Slot = e.slot
				w.Inventory = append(w.Inventory, it)
			}
		}
	}
}

func parseItem(item nbtValue) Item {
	it := Item{}
	it.ID = strOf(item.get("id"))
	if c, ok := item.get("count"); ok {
		it.Count = int(numOf(c, ok))
	} else {
		it.Count = int(numOf(item.get("Count")))
	}
	it.Slot = int(numOf(item.get("Slot")))
	if tag, ok := item.get("tag"); ok {
		it.Damage = int(numOf(tag.get("Damage")))
		if ench, ok := tag.get("Enchantments"); ok {
			for _, e := range ench.list {
				if id := strOf(e.get("id")); id != "" {
					it.Enchantments = append(it.Enchantments, id)
				}
			}
		}
	}
	if comp, ok := item.get("components"); ok {
		if dmg, ok := comp.get("minecraft:damage"); ok {
			it.Damage = int(numOf(dmg, ok))
		}
		if ench, ok := comp.get("minecraft:enchantments"); ok {
			if levels, ok := ench.get("levels"); ok {
				for k := range levels.compound {
					it.Enchantments = append(it.Enchantments, k)
				}
			}
		}
	}
	return it
}

func extractStats(w *WorldInfo, stats nbtValue) {
	for k, v := range stats.compound {
		switch k {
		case "minecraft:deaths":
			w.Deaths = v.num
		case "minecraft:mob_kills":
			w.MobKills = v.num
		case "minecraft:play_time":
			w.PlayTimeSec = v.num
		}
	}
}

func extractStatsJSON(w *WorldInfo, raw []byte) {
	var doc struct {
		Stats map[string]map[string]int64 `json:"stats"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return
	}
	custom := doc.Stats["minecraft:custom"]
	w.Deaths = custom["minecraft:deaths"]
	w.MobKills = custom["minecraft:mob_kills"]
	w.PlayTimeSec = custom["minecraft:play_time"]
}

func statsPlayTime(worldDir, uuid string) int64 {
	for _, p := range []string{
		filepath.Join(worldDir, "players", "stats", uuid+".json"),
		filepath.Join(worldDir, "stats", uuid+".json"),
	} {
		if raw, err := os.ReadFile(p); err == nil {
			var doc struct {
				Stats map[string]map[string]int64 `json:"stats"`
			}
			if json.Unmarshal(raw, &doc) == nil {
				return doc.Stats["minecraft:custom"]["minecraft:play_time"]
			}
		}
	}
	return 0
}

func strOf(v nbtValue, _ bool) string {
	s, _ := v.asStr()
	return s
}

func numOf(v nbtValue, _ bool) int64 {
	n, _ := v.asNum()
	return n
}

func fltOf(v nbtValue, _ bool) float64 {
	f, _ := v.asFlt()
	return f
}

type nbtValue struct {
	typ      byte
	str      string
	num      int64
	flt      float64
	list     []nbtValue
	compound map[string]nbtValue
	raw      []byte
}

func (v nbtValue) get(path ...string) (nbtValue, bool) {
	cur := v
	for _, p := range path {
		if cur.typ != 10 {
			return nbtValue{}, false
		}
		nv, ok := cur.compound[p]
		if !ok {
			return nbtValue{}, false
		}
		cur = nv
	}
	return cur, true
}

func (v nbtValue) asStr() (string, bool) {
	if v.typ != 8 {
		return "", false
	}
	return v.str, true
}

func (v nbtValue) asNum() (int64, bool) {
	switch v.typ {
	case 1, 2, 3, 4:
		return v.num, true
	}
	return 0, false
}

func (v nbtValue) asFlt() (float64, bool) {
	switch v.typ {
	case 5, 6:
		return v.flt, true
	}
	return 0, false
}

func parseLevelDat(path string) (nbtValue, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nbtValue{}, false
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nbtValue{}, false
	}
	defer zr.Close()
	data, err := io.ReadAll(zr)
	if err != nil {
		return nbtValue{}, false
	}
	r := &nbtReader{b: data}
	t, ok := r.u8()
	if !ok {
		return nbtValue{}, false
	}
	if _, ok := r.str(); !ok {
		return nbtValue{}, false
	}
	return r.value(t)
}

type nbtReader struct {
	b   []byte
	pos int
}

func (r *nbtReader) u8() (byte, bool) {
	if r.pos+1 > len(r.b) {
		return 0, false
	}
	v := r.b[r.pos]
	r.pos++
	return v, true
}

func (r *nbtReader) u16() (uint16, bool) {
	if r.pos+2 > len(r.b) {
		return 0, false
	}
	v := binary.BigEndian.Uint16(r.b[r.pos:])
	r.pos += 2
	return v, true
}

func (r *nbtReader) i32() (int32, bool) {
	if r.pos+4 > len(r.b) {
		return 0, false
	}
	v := int32(binary.BigEndian.Uint32(r.b[r.pos:]))
	r.pos += 4
	return v, true
}

func (r *nbtReader) i64() (int64, bool) {
	if r.pos+8 > len(r.b) {
		return 0, false
	}
	v := int64(binary.BigEndian.Uint64(r.b[r.pos:]))
	r.pos += 8
	return v, true
}

func (r *nbtReader) str() (string, bool) {
	n, ok := r.u16()
	if !ok || r.pos+int(n) > len(r.b) {
		return "", false
	}
	s := string(r.b[r.pos : r.pos+int(n)])
	r.pos += int(n)
	return s, true
}

func (r *nbtReader) value(t byte) (nbtValue, bool) {
	v := nbtValue{typ: t}
	switch t {
	case 0:
		return v, true
	case 1:
		b, ok := r.u8()
		v.num = int64(int8(b))
		return v, ok
	case 2:
		if r.pos+2 > len(r.b) {
			return v, false
		}
		v.num = int64(int16(binary.BigEndian.Uint16(r.b[r.pos:])))
		r.pos += 2
	case 3:
		i, ok := r.i32()
		v.num = int64(i)
		return v, ok
	case 4:
		i, ok := r.i64()
		v.num = i
		return v, ok
	case 5:
		if r.pos+4 > len(r.b) {
			return v, false
		}
		v.flt = float64(math.Float32frombits(binary.BigEndian.Uint32(r.b[r.pos:])))
		r.pos += 4
	case 6:
		if r.pos+8 > len(r.b) {
			return v, false
		}
		v.flt = math.Float64frombits(binary.BigEndian.Uint64(r.b[r.pos:]))
		r.pos += 8
	case 7, 11, 12:
		n, ok := r.i32()
		if !ok {
			return v, false
		}
		size := int(n)
		if t == 11 {
			size *= 4
		} else if t == 12 {
			size *= 8
		}
		if r.pos+size > len(r.b) {
			return v, false
		}
		v.raw = r.b[r.pos : r.pos+size]
		r.pos += size
	case 8:
		s, ok := r.str()
		v.str = s
		return v, ok
	case 9:
		et, ok := r.u8()
		if !ok {
			return v, false
		}
		n, ok := r.i32()
		if !ok {
			return v, false
		}
		v.list = make([]nbtValue, 0, n)
		for i := int32(0); i < n; i++ {
			item, ok := r.value(et)
			if !ok {
				return v, false
			}
			v.list = append(v.list, item)
		}
	case 10:
		v.compound = map[string]nbtValue{}
		for {
			t, ok := r.u8()
			if !ok {
				return v, false
			}
			if t == 0 {
				return v, true
			}
			name, ok := r.str()
			if !ok {
				return v, false
			}
			val, ok := r.value(t)
			if !ok {
				return v, false
			}
			v.compound[name] = val
		}
	}
	return v, true
}
