export namespace main {
	
	export class ActivityStatsAgent {
	    agent: string;
	    observedMs: number;
	    busyMs: number;
	    waitingMs: number;
	    idleMs: number;
	    waitingEvents: number;
	    sharePercent: number;
	
	    static createFrom(source: any = {}) {
	        return new ActivityStatsAgent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent = source["agent"];
	        this.observedMs = source["observedMs"];
	        this.busyMs = source["busyMs"];
	        this.waitingMs = source["waitingMs"];
	        this.idleMs = source["idleMs"];
	        this.waitingEvents = source["waitingEvents"];
	        this.sharePercent = source["sharePercent"];
	    }
	}
	export class ActivityStatsDay {
	    date: string;
	    busyMs: number;
	    waitingMs: number;
	    idleMs: number;
	
	    static createFrom(source: any = {}) {
	        return new ActivityStatsDay(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.busyMs = source["busyMs"];
	        this.waitingMs = source["waitingMs"];
	        this.idleMs = source["idleMs"];
	    }
	}
	export class ActivityStatsSession {
	    sessionId: string;
	    sessionName: string;
	    agents: string;
	    observedMs: number;
	    busyMs: number;
	    waitingMs: number;
	    idleMs: number;
	    waitingEvents: number;
	
	    static createFrom(source: any = {}) {
	        return new ActivityStatsSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.sessionName = source["sessionName"];
	        this.agents = source["agents"];
	        this.observedMs = source["observedMs"];
	        this.busyMs = source["busyMs"];
	        this.waitingMs = source["waitingMs"];
	        this.idleMs = source["idleMs"];
	        this.waitingEvents = source["waitingEvents"];
	    }
	}
	export class ActivityStatsSummary {
	    observedMs: number;
	    busyMs: number;
	    waitingMs: number;
	    idleMs: number;
	    waitingEvents: number;
	    busyPercent: number;
	
	    static createFrom(source: any = {}) {
	        return new ActivityStatsSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.observedMs = source["observedMs"];
	        this.busyMs = source["busyMs"];
	        this.waitingMs = source["waitingMs"];
	        this.idleMs = source["idleMs"];
	        this.waitingEvents = source["waitingEvents"];
	        this.busyPercent = source["busyPercent"];
	    }
	}
	export class AgentInfo {
	    type: string;
	    name: string;
	    icon: string;
	    supportsResume: boolean;
	    supportsAutoYes: boolean;
	    supportsFork: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AgentInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.name = source["name"];
	        this.icon = source["icon"];
	        this.supportsResume = source["supportsResume"];
	        this.supportsAutoYes = source["supportsAutoYes"];
	        this.supportsFork = source["supportsFork"];
	    }
	}
	export class AgentSessionInfo {
	    id: string;
	    displayName: string;
	    path: string;
	    timestamp: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentSessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.displayName = source["displayName"];
	        this.path = source["path"];
	        this.timestamp = source["timestamp"];
	    }
	}
	export class AppLog {
	    path: string;
	    lines: string[];
	    truncated: boolean;
	    missing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AppLog(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.lines = source["lines"];
	        this.truncated = source["truncated"];
	        this.missing = source["missing"];
	    }
	}
	export class BackgroundAgentInfo {
	    id: string;
	    sessionId: string;
	    pid: number;
	    cwd: string;
	    name: string;
	    status: string;
	    startedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new BackgroundAgentInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sessionId = source["sessionId"];
	        this.pid = source["pid"];
	        this.cwd = source["cwd"];
	        this.name = source["name"];
	        this.status = source["status"];
	        this.startedAt = source["startedAt"];
	    }
	}
	export class BackupInfo {
	    id: string;
	    createdAt: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new BackupInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.createdAt = source["createdAt"];
	        this.size = source["size"];
	    }
	}
	export class ClaudeUsageWindow {
	    utilization: number;
	    resetsAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ClaudeUsageWindow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.utilization = source["utilization"];
	        this.resetsAt = source["resetsAt"];
	    }
	}
	export class ClaudeUsageInfo {
	    available: boolean;
	    fiveHour: ClaudeUsageWindow;
	    sevenDay: ClaudeUsageWindow;
	    sevenDaySonnet: ClaudeUsageWindow;
	    sevenDayOpus: ClaudeUsageWindow;
	    fetchedAt: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ClaudeUsageInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.fiveHour = this.convertValues(source["fiveHour"], ClaudeUsageWindow);
	        this.sevenDay = this.convertValues(source["sevenDay"], ClaudeUsageWindow);
	        this.sevenDaySonnet = this.convertValues(source["sevenDaySonnet"], ClaudeUsageWindow);
	        this.sevenDayOpus = this.convertValues(source["sevenDayOpus"], ClaudeUsageWindow);
	        this.fetchedAt = source["fetchedAt"];
	        this.error = source["error"];
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
	
	export class CodexUsageWindow {
	    usedPercent: number;
	    windowMinutes: number;
	    resetsAt: number;
	
	    static createFrom(source: any = {}) {
	        return new CodexUsageWindow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.usedPercent = source["usedPercent"];
	        this.windowMinutes = source["windowMinutes"];
	        this.resetsAt = source["resetsAt"];
	    }
	}
	export class CodexUsageInfo {
	    available: boolean;
	    primary?: CodexUsageWindow;
	    secondary?: CodexUsageWindow;
	    planType?: string;
	    snapshotAt?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new CodexUsageInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.primary = this.convertValues(source["primary"], CodexUsageWindow);
	        this.secondary = this.convertValues(source["secondary"], CodexUsageWindow);
	        this.planType = source["planType"];
	        this.snapshotAt = source["snapshotAt"];
	        this.error = source["error"];
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
	
	export class CommandGroupInfo {
	    id: string;
	    name: string;
	    order: number;
	
	    static createFrom(source: any = {}) {
	        return new CommandGroupInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.order = source["order"];
	    }
	}
	export class SavedCommandInfo {
	    id: string;
	    name: string;
	    command: string;
	    description: string;
	    groupId: string;
	    sendEnter: boolean;
	    useCount: number;
	    placeholders: session.Placeholder[];
	
	    static createFrom(source: any = {}) {
	        return new SavedCommandInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.command = source["command"];
	        this.description = source["description"];
	        this.groupId = source["groupId"];
	        this.sendEnter = source["sendEnter"];
	        this.useCount = source["useCount"];
	        this.placeholders = this.convertValues(source["placeholders"], session.Placeholder);
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
	export class CommandLibraryInfo {
	    commands: SavedCommandInfo[];
	    groups: CommandGroupInfo[];
	
	    static createFrom(source: any = {}) {
	        return new CommandLibraryInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.commands = this.convertValues(source["commands"], SavedCommandInfo);
	        this.groups = this.convertValues(source["groups"], CommandGroupInfo);
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
	export class DictationSettings {
	    enabled: boolean;
	    googleApiKey: string;
	    language: string;
	    mode: string;
	    hotkeyCtrl: boolean;
	    hotkeyAlt: boolean;
	    hotkeyShift: boolean;
	    hotkeyKey: string;
	    muteOutputDuringRecording: boolean;
	    autoStopOnSilence: boolean;
	    silenceThreshold: number;
	    silenceDuration: number;
	    enableLogging: boolean;
	    enableDebugLogging: boolean;
	    inputDevice: string;
	    bufferMode: boolean;
	    bufferCloseOnSend: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DictationSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.googleApiKey = source["googleApiKey"];
	        this.language = source["language"];
	        this.mode = source["mode"];
	        this.hotkeyCtrl = source["hotkeyCtrl"];
	        this.hotkeyAlt = source["hotkeyAlt"];
	        this.hotkeyShift = source["hotkeyShift"];
	        this.hotkeyKey = source["hotkeyKey"];
	        this.muteOutputDuringRecording = source["muteOutputDuringRecording"];
	        this.autoStopOnSilence = source["autoStopOnSilence"];
	        this.silenceThreshold = source["silenceThreshold"];
	        this.silenceDuration = source["silenceDuration"];
	        this.enableLogging = source["enableLogging"];
	        this.enableDebugLogging = source["enableDebugLogging"];
	        this.inputDevice = source["inputDevice"];
	        this.bufferMode = source["bufferMode"];
	        this.bufferCloseOnSend = source["bufferCloseOnSend"];
	    }
	}
	export class DiffData {
	    content: string;
	    added: number;
	    removed: number;
	
	    static createFrom(source: any = {}) {
	        return new DiffData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.added = source["added"];
	        this.removed = source["removed"];
	    }
	}
	export class ForkResult {
	    sessionId: string;
	
	    static createFrom(source: any = {}) {
	        return new ForkResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	    }
	}
	export class GitBranchEntry {
	    name: string;
	    hash: string;
	    committed: string;
	    current: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GitBranchEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.hash = source["hash"];
	        this.committed = source["committed"];
	        this.current = source["current"];
	    }
	}
	export class GitBranchInfo {
	    path: string;
	    repository: boolean;
	    branch: string;
	    upstream: string;
	    ahead: number;
	    behind: number;
	
	    static createFrom(source: any = {}) {
	        return new GitBranchInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.repository = source["repository"];
	        this.branch = source["branch"];
	        this.upstream = source["upstream"];
	        this.ahead = source["ahead"];
	        this.behind = source["behind"];
	    }
	}
	export class GitBranchList {
	    path: string;
	    repository: boolean;
	    branches: GitBranchEntry[];
	    total: number;
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GitBranchList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.repository = source["repository"];
	        this.branches = this.convertValues(source["branches"], GitBranchEntry);
	        this.total = source["total"];
	        this.truncated = source["truncated"];
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
	export class GitCommit {
	    hash: string;
	    shortHash: string;
	    subject: string;
	    body: string;
	    author: string;
	    email: string;
	    committed: string;
	    refs?: string[];
	    parents?: string[];
	
	    static createFrom(source: any = {}) {
	        return new GitCommit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.shortHash = source["shortHash"];
	        this.subject = source["subject"];
	        this.body = source["body"];
	        this.author = source["author"];
	        this.email = source["email"];
	        this.committed = source["committed"];
	        this.refs = source["refs"];
	        this.parents = source["parents"];
	    }
	}
	export class GitHistoryPage {
	    path: string;
	    repository: boolean;
	    branch: string;
	    commits: GitCommit[];
	    hasMore: boolean;
	    skip: number;
	
	    static createFrom(source: any = {}) {
	        return new GitHistoryPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.repository = source["repository"];
	        this.branch = source["branch"];
	        this.commits = this.convertValues(source["commits"], GitCommit);
	        this.hasMore = source["hasMore"];
	        this.skip = source["skip"];
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
	export class GroupInfo {
	    id: string;
	    name: string;
	    collapsed: boolean;
	    color: string;
	    bgColor: string;
	    fullRowColor: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GroupInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.collapsed = source["collapsed"];
	        this.color = source["color"];
	        this.bgColor = source["bgColor"];
	        this.fullRowColor = source["fullRowColor"];
	    }
	}
	export class HistoryEntryInfo {
	    agent: string;
	    content: string;
	    sessionFile: string;
	    sessionId: string;
	    score: number;
	
	    static createFrom(source: any = {}) {
	        return new HistoryEntryInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent = source["agent"];
	        this.content = source["content"];
	        this.sessionFile = source["sessionFile"];
	        this.sessionId = source["sessionId"];
	        this.score = source["score"];
	    }
	}
	export class ImportedScheme {
	    name: string;
	    source: string;
	    colors: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ImportedScheme(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.source = source["source"];
	        this.colors = source["colors"];
	    }
	}
	export class InputDevice {
	    name: string;
	    description: string;
	    isDefault: boolean;
	
	    static createFrom(source: any = {}) {
	        return new InputDevice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.isDefault = source["isDefault"];
	    }
	}
	export class LockStatusInfo {
	    locked: boolean;
	    otherInstancePid: number;
	
	    static createFrom(source: any = {}) {
	        return new LockStatusInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.locked = source["locked"];
	        this.otherInstancePid = source["otherInstancePid"];
	    }
	}
	export class MCPSubtaskInfo {
	    id: string;
	    title: string;
	    description?: string;
	    status: string;
	    details?: string;
	
	    static createFrom(source: any = {}) {
	        return new MCPSubtaskInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.status = source["status"];
	        this.details = source["details"];
	    }
	}
	export class MCPTaskInfo {
	    id: string;
	    title: string;
	    description: string;
	    status: string;
	    priority: string;
	    tags: string[];
	    subtasks: MCPSubtaskInfo[];
	    dependencies: string[];
	    complexity?: number;
	    details?: string;
	    createdAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new MCPTaskInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.status = source["status"];
	        this.priority = source["priority"];
	        this.tags = source["tags"];
	        this.subtasks = this.convertValues(source["subtasks"], MCPSubtaskInfo);
	        this.dependencies = source["dependencies"];
	        this.complexity = source["complexity"];
	        this.details = source["details"];
	        this.createdAt = source["createdAt"];
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
	export class MultiplexerStatus {
	    available: boolean;
	    name: string;
	    version?: string;
	    hint?: string;
	    canInstall?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MultiplexerStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.name = source["name"];
	        this.version = source["version"];
	        this.hint = source["hint"];
	        this.canInstall = source["canInstall"];
	    }
	}
	export class OnlineSchemeInfo {
	    name: string;
	    file: string;
	
	    static createFrom(source: any = {}) {
	        return new OnlineSchemeInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.file = source["file"];
	    }
	}
	export class PatternRefreshResult {
	    version: number;
	    updated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PatternRefreshResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.updated = source["updated"];
	    }
	}
	export class PortableSessionInfo {
	    name: string;
	    path: string;
	    agent: string;
	    groupName: string;
	    tabs: number;
	    pathExists: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PortableSessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.agent = source["agent"];
	        this.groupName = source["groupName"];
	        this.tabs = source["tabs"];
	        this.pathExists = source["pathExists"];
	    }
	}
	export class PortableFileInfo {
	    path: string;
	    exportedAt: string;
	    appVersion: string;
	    sessions: PortableSessionInfo[];
	
	    static createFrom(source: any = {}) {
	        return new PortableFileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.exportedAt = source["exportedAt"];
	        this.appVersion = source["appVersion"];
	        this.sessions = this.convertValues(source["sessions"], PortableSessionInfo);
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
	
	export class PreviewData {
	    content: string;
	    activity: string;
	
	    static createFrom(source: any = {}) {
	        return new PreviewData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.activity = source["activity"];
	    }
	}
	export class ProjectActivityStatistics {
	    days: number;
	    recordingFrom: string;
	    updatedAt: string;
	    summary: ActivityStatsSummary;
	    series: ActivityStatsDay[];
	    agents: ActivityStatsAgent[];
	    sessions: ActivityStatsSession[];
	
	    static createFrom(source: any = {}) {
	        return new ProjectActivityStatistics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.days = source["days"];
	        this.recordingFrom = source["recordingFrom"];
	        this.updatedAt = source["updatedAt"];
	        this.summary = this.convertValues(source["summary"], ActivityStatsSummary);
	        this.series = this.convertValues(source["series"], ActivityStatsDay);
	        this.agents = this.convertValues(source["agents"], ActivityStatsAgent);
	        this.sessions = this.convertValues(source["sessions"], ActivityStatsSession);
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
	export class ProjectGitSummary {
	    sessionId: string;
	    path: string;
	    repository: boolean;
	    repositoryRoot: string;
	    branch: string;
	    upstream: string;
	    dirty: boolean;
	    modifiedFiles: number;
	    ahead: number;
	    behind: number;
	    lastCommitHash: string;
	    lastCommitMessage: string;
	    lastCommitAuthor: string;
	    lastCommitAt: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectGitSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.path = source["path"];
	        this.repository = source["repository"];
	        this.repositoryRoot = source["repositoryRoot"];
	        this.branch = source["branch"];
	        this.upstream = source["upstream"];
	        this.dirty = source["dirty"];
	        this.modifiedFiles = source["modifiedFiles"];
	        this.ahead = source["ahead"];
	        this.behind = source["behind"];
	        this.lastCommitHash = source["lastCommitHash"];
	        this.lastCommitMessage = source["lastCommitMessage"];
	        this.lastCommitAuthor = source["lastCommitAuthor"];
	        this.lastCommitAt = source["lastCommitAt"];
	        this.error = source["error"];
	    }
	}
	export class ProjectInfo {
	    id: string;
	    name: string;
	    isLocked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProjectInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.isLocked = source["isLocked"];
	    }
	}
	export class SaveFileEditResult {
	    saved: boolean;
	    conflict?: string;
	    file?: session.EditableFile;
	
	    static createFrom(source: any = {}) {
	        return new SaveFileEditResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.saved = source["saved"];
	        this.conflict = source["conflict"];
	        this.file = this.convertValues(source["file"], session.EditableFile);
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
	
	export class SessionInfo {
	    id: string;
	    name: string;
	    path: string;
	    status: string;
	    agent: string;
	    color: string;
	    bgColor: string;
	    fullRowColor: boolean;
	    groupId: string;
	    autoYes: boolean;
	    hideStatusLine: boolean;
	    notes: string;
	    favorite: boolean;
	    resumeSessionId: string;
	    followedWindows: session.FollowedWindow[];
	    mainWindowStopped: boolean;
	    tabOrder: number[];
	    extraArgs: string;
	    tabTextColor: string;
	    tabBackgroundColor: string;
	    terminalTheme: string;
	    terminalFontSize: number;
	    hideViewBar: number;
	    hideStatusBar: number;
	    mainWindowIndex: number;
	    lastWindowIndex: number;
	    isGitRepo: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.status = source["status"];
	        this.agent = source["agent"];
	        this.color = source["color"];
	        this.bgColor = source["bgColor"];
	        this.fullRowColor = source["fullRowColor"];
	        this.groupId = source["groupId"];
	        this.autoYes = source["autoYes"];
	        this.hideStatusLine = source["hideStatusLine"];
	        this.notes = source["notes"];
	        this.favorite = source["favorite"];
	        this.resumeSessionId = source["resumeSessionId"];
	        this.followedWindows = this.convertValues(source["followedWindows"], session.FollowedWindow);
	        this.mainWindowStopped = source["mainWindowStopped"];
	        this.tabOrder = source["tabOrder"];
	        this.extraArgs = source["extraArgs"];
	        this.tabTextColor = source["tabTextColor"];
	        this.tabBackgroundColor = source["tabBackgroundColor"];
	        this.terminalTheme = source["terminalTheme"];
	        this.terminalFontSize = source["terminalFontSize"];
	        this.hideViewBar = source["hideViewBar"];
	        this.hideStatusBar = source["hideStatusBar"];
	        this.mainWindowIndex = source["mainWindowIndex"];
	        this.lastWindowIndex = source["lastWindowIndex"];
	        this.isGitRepo = source["isGitRepo"];
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
	export class TemplateTabInfo {
	    name: string;
	    agent: string;
	    customCommand: string;
	    autoYes: boolean;
	    extraArgs: string;
	    workDir: string;
	
	    static createFrom(source: any = {}) {
	        return new TemplateTabInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.agent = source["agent"];
	        this.customCommand = source["customCommand"];
	        this.autoYes = source["autoYes"];
	        this.extraArgs = source["extraArgs"];
	        this.workDir = source["workDir"];
	    }
	}
	export class SessionTemplateInfo {
	    id: string;
	    name: string;
	    description: string;
	    sessionName: string;
	    path: string;
	    agent: string;
	    autoYes: boolean;
	    extraArgs: string;
	    needsPath: boolean;
	    tabs: TemplateTabInfo[];
	    useCount: number;
	
	    static createFrom(source: any = {}) {
	        return new SessionTemplateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.sessionName = source["sessionName"];
	        this.path = source["path"];
	        this.agent = source["agent"];
	        this.autoYes = source["autoYes"];
	        this.extraArgs = source["extraArgs"];
	        this.needsPath = source["needsPath"];
	        this.tabs = this.convertValues(source["tabs"], TemplateTabInfo);
	        this.useCount = source["useCount"];
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
	export class SettingsInfo {
	    compactList: boolean;
	    hideStatusLines: boolean;
	    showAgentIcons: boolean;
	    hideYoloBadge: boolean;
	    showResumeBadge: boolean;
	    splitView: boolean;
	    markedSessionId: string;
	    lastSessionId: string;
	    markedWindowIdx: number;
	    language: string;
	    uiTheme: string;
	    uiAccent: string;
	    terminalRenderer: string;
	    terminalCopyMode: string;
	    terminalFontFamily: string;
	    terminalShell: string;
	    shellChoices: session.ShellChoice[];
	    gitBranchDisplay: string;
	    diffFlatFileList: boolean;
	    trashRetentionDays: number;
	    taskMasterEnabled: boolean;
	    restoreLastSession: boolean;
	    terminalFontSize: number;
	    agentFontSize: number;
	    hideViewBar: boolean;
	    agentHideViewBar: boolean;
	    hideStatusBar: boolean;
	    agentHideStatusBar: boolean;
	    notifyOnWaiting: boolean;
	    notifyDesktop: boolean;
	    notifyNtfy: boolean;
	    ntfyUrl: string;
	    terminalTheme: string;
	    agentDefaultTheme: string;
	    shortcutOverrides: Record<string, any>;
	    diffAboveHeight: number;
	    dictationBuffer?: session.PanelGeometry;
	    diffSideBySide: boolean;
	    diffLastFile: Record<string, string>;
	    agentTerminalThemes: Record<string, string>;
	    customTerminalThemes: session.CustomTerminalTheme[];
	
	    static createFrom(source: any = {}) {
	        return new SettingsInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.compactList = source["compactList"];
	        this.hideStatusLines = source["hideStatusLines"];
	        this.showAgentIcons = source["showAgentIcons"];
	        this.hideYoloBadge = source["hideYoloBadge"];
	        this.showResumeBadge = source["showResumeBadge"];
	        this.splitView = source["splitView"];
	        this.markedSessionId = source["markedSessionId"];
	        this.lastSessionId = source["lastSessionId"];
	        this.markedWindowIdx = source["markedWindowIdx"];
	        this.language = source["language"];
	        this.uiTheme = source["uiTheme"];
	        this.uiAccent = source["uiAccent"];
	        this.terminalRenderer = source["terminalRenderer"];
	        this.terminalCopyMode = source["terminalCopyMode"];
	        this.terminalFontFamily = source["terminalFontFamily"];
	        this.terminalShell = source["terminalShell"];
	        this.shellChoices = this.convertValues(source["shellChoices"], session.ShellChoice);
	        this.gitBranchDisplay = source["gitBranchDisplay"];
	        this.diffFlatFileList = source["diffFlatFileList"];
	        this.trashRetentionDays = source["trashRetentionDays"];
	        this.taskMasterEnabled = source["taskMasterEnabled"];
	        this.restoreLastSession = source["restoreLastSession"];
	        this.terminalFontSize = source["terminalFontSize"];
	        this.agentFontSize = source["agentFontSize"];
	        this.hideViewBar = source["hideViewBar"];
	        this.agentHideViewBar = source["agentHideViewBar"];
	        this.hideStatusBar = source["hideStatusBar"];
	        this.agentHideStatusBar = source["agentHideStatusBar"];
	        this.notifyOnWaiting = source["notifyOnWaiting"];
	        this.notifyDesktop = source["notifyDesktop"];
	        this.notifyNtfy = source["notifyNtfy"];
	        this.ntfyUrl = source["ntfyUrl"];
	        this.terminalTheme = source["terminalTheme"];
	        this.agentDefaultTheme = source["agentDefaultTheme"];
	        this.shortcutOverrides = source["shortcutOverrides"];
	        this.diffAboveHeight = source["diffAboveHeight"];
	        this.dictationBuffer = this.convertValues(source["dictationBuffer"], session.PanelGeometry);
	        this.diffSideBySide = source["diffSideBySide"];
	        this.diffLastFile = source["diffLastFile"];
	        this.agentTerminalThemes = source["agentTerminalThemes"];
	        this.customTerminalThemes = this.convertValues(source["customTerminalThemes"], session.CustomTerminalTheme);
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
	export class SidebarUpdate {
	    activities: Record<string, string>;
	    statusLines: Record<string, string>;
	    spinnerTexts: Record<string, string>;
	    tabStatuses: Record<string, Array<TabStatusInfo>>;
	
	    static createFrom(source: any = {}) {
	        return new SidebarUpdate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.activities = source["activities"];
	        this.statusLines = source["statusLines"];
	        this.spinnerTexts = source["spinnerTexts"];
	        this.tabStatuses = this.convertValues(source["tabStatuses"], Array<TabStatusInfo>, true);
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
	export class SubtaskInfo {
	    id: string;
	    title: string;
	    done: boolean;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SubtaskInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.done = source["done"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class TabStatusInfo {
	    windowIdx: number;
	    agent: string;
	    name: string;
	    activity: string;
	    statusLine: string;
	    spinnerText: string;
	    yolo: boolean;
	    hideStatusLine: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TabStatusInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.windowIdx = source["windowIdx"];
	        this.agent = source["agent"];
	        this.name = source["name"];
	        this.activity = source["activity"];
	        this.statusLine = source["statusLine"];
	        this.spinnerText = source["spinnerText"];
	        this.yolo = source["yolo"];
	        this.hideStatusLine = source["hideStatusLine"];
	    }
	}
	export class TaskInfo {
	    id: string;
	    title: string;
	    description: string;
	    details?: string;
	    status: string;
	    priority: string;
	    tags: string[];
	    subtasks: SubtaskInfo[];
	    dependencies: string[];
	    createdAt: string;
	    updatedAt: string;
	    completedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.details = source["details"];
	        this.status = source["status"];
	        this.priority = source["priority"];
	        this.tags = source["tags"];
	        this.subtasks = this.convertValues(source["subtasks"], SubtaskInfo);
	        this.dependencies = source["dependencies"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.completedAt = source["completedAt"];
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
	
	export class TerminalServer {
	
	
	    static createFrom(source: any = {}) {
	        return new TerminalServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class TrashItemInfo {
	    id: string;
	    kind: string;
	    name: string;
	    parentSessionId: string;
	    parentSessionName: string;
	    deletedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new TrashItemInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.name = source["name"];
	        this.parentSessionId = source["parentSessionId"];
	        this.parentSessionName = source["parentSessionName"];
	        this.deletedAt = source["deletedAt"];
	    }
	}
	export class UpdateInfo {
	    available: boolean;
	    currentVersion: string;
	    latestVersion: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	    }
	}

}

export namespace session {
	
	export class BrowseEntry {
	    name: string;
	    path: string;
	    isDir: boolean;
	    size: number;
	    modTime: string;
	    unreadable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BrowseEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.size = source["size"];
	        this.modTime = source["modTime"];
	        this.unreadable = source["unreadable"];
	    }
	}
	export class BrowseFile {
	    path: string;
	    absPath: string;
	    content: string;
	    size: number;
	    binary: boolean;
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BrowseFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.absPath = source["absPath"];
	        this.content = source["content"];
	        this.size = source["size"];
	        this.binary = source["binary"];
	        this.truncated = source["truncated"];
	    }
	}
	export class BrowseListing {
	    path: string;
	    absPath: string;
	    entries: BrowseEntry[];
	    truncated: boolean;
	    totalEntries: number;
	
	    static createFrom(source: any = {}) {
	        return new BrowseListing(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.absPath = source["absPath"];
	        this.entries = this.convertValues(source["entries"], BrowseEntry);
	        this.truncated = source["truncated"];
	        this.totalEntries = source["totalEntries"];
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
	export class ContentMatch {
	    path: string;
	    line: number;
	    text: string;
	    col: number;
	    length: number;
	
	    static createFrom(source: any = {}) {
	        return new ContentMatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.line = source["line"];
	        this.text = source["text"];
	        this.col = source["col"];
	        this.length = source["length"];
	    }
	}
	export class ContentSearchResult {
	    matches: ContentMatch[];
	    truncated: boolean;
	    filesSearched: number;
	    filesSkipped: number;
	
	    static createFrom(source: any = {}) {
	        return new ContentSearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.matches = this.convertValues(source["matches"], ContentMatch);
	        this.truncated = source["truncated"];
	        this.filesSearched = source["filesSearched"];
	        this.filesSkipped = source["filesSkipped"];
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
	export class CustomTerminalTheme {
	    id: string;
	    name: string;
	    colors: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new CustomTerminalTheme(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.colors = source["colors"];
	    }
	}
	export class DiffHunk {
	    header: string;
	    body: string;
	    index: number;
	    added: number;
	    removed: number;
	    patch: string;
	
	    static createFrom(source: any = {}) {
	        return new DiffHunk(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.header = source["header"];
	        this.body = source["body"];
	        this.index = source["index"];
	        this.added = source["added"];
	        this.removed = source["removed"];
	        this.patch = source["patch"];
	    }
	}
	export class DiffFile {
	    path: string;
	    oldPath: string;
	    status: string;
	    header: string;
	    hunks: DiffHunk[];
	    added: number;
	    removed: number;
	    binary: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DiffFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.oldPath = source["oldPath"];
	        this.status = source["status"];
	        this.header = source["header"];
	        this.hunks = this.convertValues(source["hunks"], DiffHunk);
	        this.added = source["added"];
	        this.removed = source["removed"];
	        this.binary = source["binary"];
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
	export class DiffFileSummary {
	    path: string;
	    oldPath: string;
	    status: string;
	    added: number;
	    removed: number;
	    binary: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DiffFileSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.oldPath = source["oldPath"];
	        this.status = source["status"];
	        this.added = source["added"];
	        this.removed = source["removed"];
	        this.binary = source["binary"];
	    }
	}
	
	export class FileShape {
	    bom: boolean;
	    lineEnding: string;
	    mixed: boolean;
	    trailingNewline: boolean;
	    empty: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileShape(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bom = source["bom"];
	        this.lineEnding = source["lineEnding"];
	        this.mixed = source["mixed"];
	        this.trailingNewline = source["trailingNewline"];
	        this.empty = source["empty"];
	    }
	}
	export class EditableFile {
	    path: string;
	    absPath: string;
	    text: string;
	    shape: FileShape;
	    version: string;
	    mode: number;
	    size: number;
	    editable: boolean;
	    notEditableReason?: string;
	
	    static createFrom(source: any = {}) {
	        return new EditableFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.absPath = source["absPath"];
	        this.text = source["text"];
	        this.shape = this.convertValues(source["shape"], FileShape);
	        this.version = source["version"];
	        this.mode = source["mode"];
	        this.size = source["size"];
	        this.editable = source["editable"];
	        this.notEditableReason = source["notEditableReason"];
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
	export class IndexedFile {
	    path: string;
	    name: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new IndexedFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.size = source["size"];
	    }
	}
	export class FileIndex {
	    files: IndexedFile[];
	    truncated: boolean;
	    skippedDirs: string[];
	    includedAll: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileIndex(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.files = this.convertValues(source["files"], IndexedFile);
	        this.truncated = source["truncated"];
	        this.skippedDirs = source["skippedDirs"];
	        this.includedAll = source["includedAll"];
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
	
	export class FollowedWindow {
	    index: number;
	    agent: string;
	    name: string;
	    custom_command: string;
	    auto_yes: boolean;
	    resume_session_id: string;
	    notes?: string;
	    extra_args?: string;
	    stopped?: boolean;
	    terminal_theme?: string;
	    terminal_font_size?: number;
	    hide_view_bar?: number;
	    hide_status_bar?: number;
	    text_color?: string;
	    background_color?: string;
	    work_dir?: string;
	    hide_status_line?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FollowedWindow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.agent = source["agent"];
	        this.name = source["name"];
	        this.custom_command = source["custom_command"];
	        this.auto_yes = source["auto_yes"];
	        this.resume_session_id = source["resume_session_id"];
	        this.notes = source["notes"];
	        this.extra_args = source["extra_args"];
	        this.stopped = source["stopped"];
	        this.terminal_theme = source["terminal_theme"];
	        this.terminal_font_size = source["terminal_font_size"];
	        this.hide_view_bar = source["hide_view_bar"];
	        this.hide_status_bar = source["hide_status_bar"];
	        this.text_color = source["text_color"];
	        this.background_color = source["background_color"];
	        this.work_dir = source["work_dir"];
	        this.hide_status_line = source["hide_status_line"];
	    }
	}
	
	export class PanelGeometry {
	    x: number;
	    y: number;
	    w: number;
	    h: number;
	
	    static createFrom(source: any = {}) {
	        return new PanelGeometry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.w = source["w"];
	        this.h = source["h"];
	    }
	}
	export class Placeholder {
	    name: string;
	    default?: string;
	
	    static createFrom(source: any = {}) {
	        return new Placeholder(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.default = source["default"];
	    }
	}
	export class QuickJumpEntry {
	    sessionId: string;
	    windowIdx: number;
	    label?: string;
	
	    static createFrom(source: any = {}) {
	        return new QuickJumpEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.windowIdx = source["windowIdx"];
	        this.label = source["label"];
	    }
	}
	export class RestoreResult {
	    sessionId: string;
	    windowIdx: number;
	
	    static createFrom(source: any = {}) {
	        return new RestoreResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.windowIdx = source["windowIdx"];
	    }
	}
	export class ShellChoice {
	    command: string;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new ShellChoice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.command = source["command"];
	        this.label = source["label"];
	    }
	}
	export class WindowInfo {
	    Index: number;
	    Name: string;
	    Active: boolean;
	    Followed: boolean;
	    Agent: string;
	    Dead: boolean;
	    TextColor: string;
	    BackgroundColor: string;
	
	    static createFrom(source: any = {}) {
	        return new WindowInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Index = source["Index"];
	        this.Name = source["Name"];
	        this.Active = source["Active"];
	        this.Followed = source["Followed"];
	        this.Agent = source["Agent"];
	        this.Dead = source["Dead"];
	        this.TextColor = source["TextColor"];
	        this.BackgroundColor = source["BackgroundColor"];
	    }
	}

}

