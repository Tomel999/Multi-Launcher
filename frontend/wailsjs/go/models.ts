export namespace launcher {
	
	export class CleanReport {
	    files: number;
	    freed: number;
	
	    static createFrom(source: any = {}) {
	        return new CleanReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.files = source["files"];
	        this.freed = source["freed"];
	    }
	}
	export class ClientInfo {
	    id: string;
	    name: string;
	    hasModules: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ClientInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.hasModules = source["hasModules"];
	    }
	}
	export class ClientVersion {
	    id: string;
	    modules: string[];
	    lunarOnly: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ClientVersion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.modules = source["modules"];
	        this.lunarOnly = source["lunarOnly"];
	    }
	}
	export class DeviceCode {
	    device_code: string;
	    user_code: string;
	    verification_uri: string;
	    expires_in: number;
	    interval: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new DeviceCode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.device_code = source["device_code"];
	        this.user_code = source["user_code"];
	        this.verification_uri = source["verification_uri"];
	        this.expires_in = source["expires_in"];
	        this.interval = source["interval"];
	        this.message = source["message"];
	    }
	}
	export class FileProgress {
	    name: string;
	    bytes: number;
	    size: number;
	    speed: number;
	
	    static createFrom(source: any = {}) {
	        return new FileProgress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.bytes = source["bytes"];
	        this.size = source["size"];
	        this.speed = source["speed"];
	    }
	}
	export class Item {
	    id: string;
	    count: number;
	    slot: number;
	    damage: number;
	    enchantments: string[];
	
	    static createFrom(source: any = {}) {
	        return new Item(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.count = source["count"];
	        this.slot = source["slot"];
	        this.damage = source["damage"];
	        this.enchantments = source["enchantments"];
	    }
	}
	export class LaunchOptions {
	    memory: string;
	    minMemory: string;
	    jvmArgs: string;
	    javaPath: string;
	    width: number;
	    height: number;
	    fullscreen: boolean;
	    server: string;
	    env: string[];
	    preLaunch: string;
	    postExit: string;
	    accountId: string;
	
	    static createFrom(source: any = {}) {
	        return new LaunchOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.memory = source["memory"];
	        this.minMemory = source["minMemory"];
	        this.jvmArgs = source["jvmArgs"];
	        this.javaPath = source["javaPath"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.fullscreen = source["fullscreen"];
	        this.server = source["server"];
	        this.env = source["env"];
	        this.preLaunch = source["preLaunch"];
	        this.postExit = source["postExit"];
	        this.accountId = source["accountId"];
	    }
	}
	export class LocalSkin {
	    name: string;
	    size: number;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new LocalSkin(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.size = source["size"];
	        this.url = source["url"];
	    }
	}
	export class ModInfo {
	    name: string;
	    icon: string;
	
	    static createFrom(source: any = {}) {
	        return new ModInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.icon = source["icon"];
	    }
	}
	export class ModpackInfo {
	    mcVersion: string;
	    loader: string;
	    loaderVersion: string;
	
	    static createFrom(source: any = {}) {
	        return new ModpackInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mcVersion = source["mcVersion"];
	        this.loader = source["loader"];
	        this.loaderVersion = source["loaderVersion"];
	    }
	}
	export class Option {
	    key: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new Option(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	    }
	}
	export class Plugin {
	    name: string;
	    filename: string;
	    disabled: boolean;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new Plugin(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.filename = source["filename"];
	        this.disabled = source["disabled"];
	        this.size = source["size"];
	    }
	}
	export class Progress {
	    phase: string;
	    current: number;
	    total: number;
	    bytes: number;
	    totalBytes: number;
	    speed: number;
	    file: string;
	    files: FileProgress[];
	
	    static createFrom(source: any = {}) {
	        return new Progress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.current = source["current"];
	        this.total = source["total"];
	        this.bytes = source["bytes"];
	        this.totalBytes = source["totalBytes"];
	        this.speed = source["speed"];
	        this.file = source["file"];
	        this.files = this.convertValues(source["files"], FileProgress);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PruneReport {
	    version: string;
	    removed: string[];
	    freedBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new PruneReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.removed = source["removed"];
	        this.freedBytes = source["freedBytes"];
	    }
	}
	export class Screenshot {
	    name: string;
	    size: number;
	    modified: number;
	
	    static createFrom(source: any = {}) {
	        return new Screenshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.size = source["size"];
	        this.modified = source["modified"];
	    }
	}
	export class State {
	    running: boolean;
	    pid: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.pid = source["pid"];
	        this.error = source["error"];
	    }
	}
	export class StorageCategory {
	    name: string;
	    path: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new StorageCategory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.size = source["size"];
	    }
	}
	export class World {
	    name: string;
	    displayName: string;
	    icon: string;
	    lastPlayed: number;
	    playTime: number;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new World(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.icon = source["icon"];
	        this.lastPlayed = source["lastPlayed"];
	        this.playTime = source["playTime"];
	        this.size = source["size"];
	    }
	}
	export class WorldInfo {
	    name: string;
	    displayName: string;
	    icon: string;
	    lastPlayed: number;
	    playTime: number;
	    size: number;
	    seed: number;
	    version: string;
	    difficulty: number;
	    hardcore: boolean;
	    gameMode: number;
	    days: number;
	    pos: number[];
	    dimension: string;
	    health: number;
	    foodLevel: number;
	    xpLevel: number;
	    deaths: number;
	    mobKills: number;
	    playTimeSec: number;
	    inventory: Item[];
	    playerUuid: string;
	
	    static createFrom(source: any = {}) {
	        return new WorldInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.icon = source["icon"];
	        this.lastPlayed = source["lastPlayed"];
	        this.playTime = source["playTime"];
	        this.size = source["size"];
	        this.seed = source["seed"];
	        this.version = source["version"];
	        this.difficulty = source["difficulty"];
	        this.hardcore = source["hardcore"];
	        this.gameMode = source["gameMode"];
	        this.days = source["days"];
	        this.pos = source["pos"];
	        this.dimension = source["dimension"];
	        this.health = source["health"];
	        this.foodLevel = source["foodLevel"];
	        this.xpLevel = source["xpLevel"];
	        this.deaths = source["deaths"];
	        this.mobKills = source["mobKills"];
	        this.playTimeSec = source["playTimeSec"];
	        this.inventory = this.convertValues(source["inventory"], Item);
	        this.playerUuid = source["playerUuid"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class PubAccount {
	    id: string;
	    name: string;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new PubAccount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.type = source["type"];
	    }
	}
	export class VerifiedCatalog {
	    repo: string;
	    skipped?: string[];
	    categories: Record<string, Array<VerifiedPlugin>>;
	
	    static createFrom(source: any = {}) {
	        return new VerifiedCatalog(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repo = source["repo"];
	        this.skipped = source["skipped"];
	        this.categories = this.convertValues(source["categories"], Array<VerifiedPlugin>, true);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class VerifiedPlugin {
	    id: string;
	    name: string;
	    version: string;
	    category: string;
	    author?: string;
	    description?: string;
	    downloadUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new VerifiedPlugin(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.version = source["version"];
	        this.category = source["category"];
	        this.author = source["author"];
	        this.description = source["description"];
	        this.downloadUrl = source["downloadUrl"];
	    }
	}

}

export namespace panorama {
	
	export class VersionEntry {
	    id: string;
	    type: string;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new VersionEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.url = source["url"];
	    }
	}

}

export namespace plugin {
	
	export class SettingField {
	    key: string;
	    type: string;
	    label: string;
	    default?: any;
	    options?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SettingField(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.type = source["type"];
	        this.label = source["label"];
	        this.default = source["default"];
	        this.options = source["options"];
	    }
	}
	export class Manifest {
	    id: string;
	    name: string;
	    version: string;
	    category: string;
	    runtime?: string;
	    entry?: string;
	    frontend?: string;
	    description?: string;
	    author?: string;
	    homepage?: string;
	    minLauncherVersion?: string;
	    permissions?: string[];
	    settings?: SettingField[];
	
	    static createFrom(source: any = {}) {
	        return new Manifest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.version = source["version"];
	        this.category = source["category"];
	        this.runtime = source["runtime"];
	        this.entry = source["entry"];
	        this.frontend = source["frontend"];
	        this.description = source["description"];
	        this.author = source["author"];
	        this.homepage = source["homepage"];
	        this.minLauncherVersion = source["minLauncherVersion"];
	        this.permissions = source["permissions"];
	        this.settings = this.convertValues(source["settings"], SettingField);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class InstalledPlugin {
	    manifest?: Manifest;
	    dir: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new InstalledPlugin(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.manifest = this.convertValues(source["manifest"], Manifest);
	        this.dir = source["dir"];
	        this.enabled = source["enabled"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	

}

export namespace updater {
	
	export class Managed {
	    id: string;
	    name: string;
	    command: string;
	
	    static createFrom(source: any = {}) {
	        return new Managed(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.command = source["command"];
	    }
	}
	export class UpdateInfo {
	    current: string;
	    version: string;
	    tagName: string;
	    name: string;
	    changelog: string;
	    publishedAt: string;
	    releaseUrl: string;
	    assetName: string;
	    size: number;
	    managed?: Managed;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.version = source["version"];
	        this.tagName = source["tagName"];
	        this.name = source["name"];
	        this.changelog = source["changelog"];
	        this.publishedAt = source["publishedAt"];
	        this.releaseUrl = source["releaseUrl"];
	        this.assetName = source["assetName"];
	        this.size = source["size"];
	        this.managed = this.convertValues(source["managed"], Managed);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

