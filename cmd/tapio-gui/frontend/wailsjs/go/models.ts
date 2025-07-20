export namespace main {
	
	export class Action {
	    id: string;
	    title: string;
	    description: string;
	    commands: string[];
	    risk: string;
	    auto_apply: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Action(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.commands = source["commands"];
	        this.risk = source["risk"];
	        this.auto_apply = source["auto_apply"];
	    }
	}
	export class ClusterStatus {
	    status: string;
	    nodes_total: number;
	    nodes_ready: number;
	    pods_total: number;
	    pods_healthy: number;
	    stories_total: number;
	    stories_critical: number;
	    stories_resolved: number;
	    last_update: time.Time;
	
	    static createFrom(source: any = {}) {
	        return new ClusterStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.nodes_total = source["nodes_total"];
	        this.nodes_ready = source["nodes_ready"];
	        this.pods_total = source["pods_total"];
	        this.pods_healthy = source["pods_healthy"];
	        this.stories_total = source["stories_total"];
	        this.stories_critical = source["stories_critical"];
	        this.stories_resolved = source["stories_resolved"];
	        this.last_update = this.convertValues(source["last_update"], time.Time);
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
	export class HealthResponse {
	    status: string;
	    message: string;
	    timestamp: time.Time;
	
	    static createFrom(source: any = {}) {
	        return new HealthResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.message = source["message"];
	        this.timestamp = this.convertValues(source["timestamp"], time.Time);
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
	export class LogField {
	    key: string;
	    value: any;
	
	    static createFrom(source: any = {}) {
	        return new LogField(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	    }
	}
	export class SpanRef {
	    refType: string;
	    traceId: string;
	    spanId: string;
	
	    static createFrom(source: any = {}) {
	        return new SpanRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.refType = source["refType"];
	        this.traceId = source["traceId"];
	        this.spanId = source["spanId"];
	    }
	}
	export class SpanProcess {
	    serviceName: string;
	    tags: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new SpanProcess(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serviceName = source["serviceName"];
	        this.tags = source["tags"];
	    }
	}
	export class SpanLog {
	    timestamp: number;
	    fields: LogField[];
	
	    static createFrom(source: any = {}) {
	        return new SpanLog(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.fields = this.convertValues(source["fields"], LogField);
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
	export class OTELSpan {
	    spanId: string;
	    traceId: string;
	    operationName: string;
	    serviceName: string;
	    startTime: number;
	    duration: number;
	    tags: Record<string, any>;
	    logs: SpanLog[];
	    process: SpanProcess;
	    references?: SpanRef[];
	    storyId?: string;
	    correlationId?: string;
	    severity?: string;
	    pattern?: string;
	
	    static createFrom(source: any = {}) {
	        return new OTELSpan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.spanId = source["spanId"];
	        this.traceId = source["traceId"];
	        this.operationName = source["operationName"];
	        this.serviceName = source["serviceName"];
	        this.startTime = source["startTime"];
	        this.duration = source["duration"];
	        this.tags = source["tags"];
	        this.logs = this.convertValues(source["logs"], SpanLog);
	        this.process = this.convertValues(source["process"], SpanProcess);
	        this.references = this.convertValues(source["references"], SpanRef);
	        this.storyId = source["storyId"];
	        this.correlationId = source["correlationId"];
	        this.severity = source["severity"];
	        this.pattern = source["pattern"];
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
	export class OTELTrace {
	    traceId: string;
	    spanCount: number;
	    serviceName: string;
	    operationName: string;
	    duration: number;
	    startTime: time.Time;
	    spans: OTELSpan[];
	    tags: Record<string, any>;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new OTELTrace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.traceId = source["traceId"];
	        this.spanCount = source["spanCount"];
	        this.serviceName = source["serviceName"];
	        this.operationName = source["operationName"];
	        this.duration = source["duration"];
	        this.startTime = this.convertValues(source["startTime"], time.Time);
	        this.spans = this.convertValues(source["spans"], OTELSpan);
	        this.tags = source["tags"];
	        this.warnings = source["warnings"];
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
	export class Resource {
	    type: string;
	    name: string;
	    namespace: string;
	
	    static createFrom(source: any = {}) {
	        return new Resource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.name = source["name"];
	        this.namespace = source["namespace"];
	    }
	}
	
	
	
	export class Story {
	    id: string;
	    title: string;
	    description: string;
	    severity: string;
	    category: string;
	    timestamp: time.Time;
	    resources: Resource[];
	    actions: Action[];
	    root_cause?: string;
	    prediction?: string;
	
	    static createFrom(source: any = {}) {
	        return new Story(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.severity = source["severity"];
	        this.category = source["category"];
	        this.timestamp = this.convertValues(source["timestamp"], time.Time);
	        this.resources = this.convertValues(source["resources"], Resource);
	        this.actions = this.convertValues(source["actions"], Action);
	        this.root_cause = source["root_cause"];
	        this.prediction = source["prediction"];
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
	export class WebSocketHub {
	
	
	    static createFrom(source: any = {}) {
	        return new WebSocketHub(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace time {
	
	export class Time {
	
	
	    static createFrom(source: any = {}) {
	        return new Time(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

