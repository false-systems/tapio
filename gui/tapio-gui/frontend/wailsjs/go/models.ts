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
	    // Go type: time
	    last_update: any;
	
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
	        this.last_update = this.convertValues(source["last_update"], null);
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
	    // Go type: time
	    timestamp: any;
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
	        this.timestamp = this.convertValues(source["timestamp"], null);
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

}

