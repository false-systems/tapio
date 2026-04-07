use std::sync::Arc;

use dashmap::DashMap;
use futures::StreamExt;
use k8s_openapi::api::core::v1::Pod;
use kube::runtime::{reflector, watcher};
use kube::{Api, Client};
use tapio_common::occurrence::{Context, Entity};

pub struct K8sEnricher {
    store: reflector::Store<Pod>,
    node_name: String,
    /// cgroup_id → pod UID cache (None = resolved but no pod found)
    cgroup_cache: Arc<DashMap<u64, Option<String>>>,
}

impl K8sEnricher {
    pub async fn new() -> anyhow::Result<Self> {
        let node_name = std::env::var("NODE_NAME")
            .map_err(|_| anyhow::anyhow!("NODE_NAME env var required for K8s enrichment"))?;

        let client = Client::try_default().await?;
        let pods: Api<Pod> = Api::all(client);

        let wc = watcher::Config::default().fields(&format!("spec.nodeName={node_name}"));

        let (store, writer) = reflector::store();
        let stream = reflector::reflector(writer, watcher(pods, wc));

        let cgroup_cache = Arc::new(DashMap::new());
        let cache_for_reflector = cgroup_cache.clone();

        tokio::spawn(async move {
            futures::pin_mut!(stream);
            while let Some(event) = stream.next().await {
                match event {
                    Ok(watcher::Event::Delete(pod)) => {
                        if let Some(uid) = pod.metadata.uid.as_deref() {
                            let uid_owned = uid.to_string();
                            cache_for_reflector.retain(|_: &u64, v: &mut Option<String>| {
                                v.as_deref() != Some(uid_owned.as_str())
                            });
                            tracing::debug!(pod_uid = uid, "cleaned stale cgroup cache entries");
                        }
                    }
                    Err(e) => tracing::warn!(error = %e, "pod reflector error"),
                    _ => {}
                }
            }
        });

        tracing::info!(node = %node_name, "K8s enricher started");

        Ok(Self {
            store,
            node_name,
            cgroup_cache,
        })
    }

    /// Enrich an occurrence with K8s pod context using the cgroup ID.
    /// Returns None if cgroup_id is 0, pod is not found, or resolution fails.
    pub fn enrich(&self, cgroup_id: u64) -> Option<Context> {
        if cgroup_id == 0 {
            return None;
        }

        // Check cache
        if let Some(cached) = self.cgroup_cache.get(&cgroup_id) {
            return cached.as_ref().and_then(|uid| self.context_for_uid(uid));
        }

        // Try to resolve cgroup_id → pod UID via filesystem stat
        let pod_uid = self.resolve_cgroup_id(cgroup_id);
        self.cgroup_cache.insert(cgroup_id, pod_uid.clone());

        pod_uid.and_then(|uid| self.context_for_uid(&uid))
    }

    /// Enrich an occurrence using PID (reads /proc/<pid>/cgroup).
    /// Used by the network observer which doesn't have cgroup_id.
    pub fn enrich_by_pid(&self, pid: u32) -> Option<Context> {
        let content = std::fs::read_to_string(format!("/proc/{pid}/cgroup")).ok()?;
        let pod_uid = parse_pod_uid(&content)?;
        self.context_for_uid(&pod_uid)
    }

    /// Resolve cgroup_id to pod UID by statting cgroup directories for all known pods.
    fn resolve_cgroup_id(&self, target_id: u64) -> Option<String> {
        use std::os::unix::fs::MetadataExt;

        for pod in self.store.state() {
            let Some(uid) = pod.metadata.uid.as_deref() else {
                continue;
            };
            let qos = pod_qos(&pod);
            let uid_underscored = uid.replace('-', "_");

            for path in [
                format!("/sys/fs/cgroup/kubepods/{qos}/pod{uid}"),
                format!(
                    "/sys/fs/cgroup/kubepods.slice/kubepods-{qos}.slice/kubepods-{qos}-pod{uid_underscored}.slice"
                ),
            ] {
                if let Ok(meta) = std::fs::metadata(&path) {
                    let ino = meta.ino();
                    // Cache every pod we resolve, not just the target
                    self.cgroup_cache.insert(ino, Some(uid.to_string()));
                    if ino == target_id {
                        return Some(uid.to_string());
                    }
                }
            }
        }
        None
    }

    fn context_for_uid(&self, uid: &str) -> Option<Context> {
        let pods = self.store.state();
        let pod = pods
            .iter()
            .find(|p| p.metadata.uid.as_deref() == Some(uid))?;

        let namespace = pod.metadata.namespace.as_deref().unwrap_or_default();
        let name = pod.metadata.name.as_deref().unwrap_or_default();
        let resource_version = pod.metadata.resource_version.clone();

        let mut entities = vec![Entity {
            kind: "pod".into(),
            id: format!("{namespace}/{name}"),
            name: Some(name.to_string()),
            version: resource_version,
        }];

        // Derive deployment name from ReplicaSet owner reference
        if let Some(owners) = &pod.metadata.owner_references {
            for owner in owners {
                if owner.kind == "ReplicaSet"
                    && let Some((deploy, _)) = owner.name.rsplit_once('-')
                {
                    entities.push(Entity {
                        kind: "deployment".into(),
                        id: format!("{namespace}/{deploy}"),
                        name: Some(deploy.to_string()),
                        version: None,
                    });
                }
            }
        }

        Some(Context {
            cluster: None,
            node: Some(self.node_name.clone()),
            namespace: Some(namespace.to_string()),
            trace_id: None,
            span_id: None,
            entities,
            correlation_keys: vec![],
        })
    }
}

fn pod_qos(pod: &Pod) -> String {
    pod.status
        .as_ref()
        .and_then(|s| s.qos_class.as_deref())
        .unwrap_or("BestEffort")
        .to_lowercase()
}

/// Parse pod UID from /proc/<pid>/cgroup content.
///
/// cgroupv2: `0::/kubepods/<qos>/pod<uid>/<container-id>`
/// cgroupv1: `...:cpuacct,cpu:/kubepods/<qos>/pod<uid>/<container-id>`
/// systemd:  `0::/kubepods.slice/kubepods-<qos>.slice/kubepods-<qos>-pod<uid>.slice/...`
pub fn parse_pod_uid(cgroup_content: &str) -> Option<String> {
    for line in cgroup_content.lines() {
        // Cgroup path is after the last ':'
        let path = line.rsplit(':').next()?;

        // Standard format: /kubepods/<qos>/pod<uid>/<container-id>
        if let Some(idx) = path.find("/pod") {
            let after_pod = &path[idx + 4..];
            let uid_end = after_pod.find('/').unwrap_or(after_pod.len());
            let uid = &after_pod[..uid_end];
            if !uid.is_empty() {
                return Some(uid.to_string());
            }
        }

        // Systemd slice format: kubepods-<qos>-pod<uid>.slice
        if let Some(idx) = path.find("-pod") {
            let after_pod = &path[idx + 4..];
            let uid_end = after_pod.find('.').unwrap_or(after_pod.len());
            let uid = &after_pod[..uid_end];
            if !uid.is_empty() {
                // Systemd uses underscores instead of dashes in UIDs
                return Some(uid.replace('_', "-"));
            }
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_pod_uid_cgroupv2() {
        let content = "0::/kubepods/burstable/pod1a2b3c4d-5e6f-7890-abcd-ef1234567890/abc123\n";
        assert_eq!(
            parse_pod_uid(content).as_deref(),
            Some("1a2b3c4d-5e6f-7890-abcd-ef1234567890"),
        );
    }

    #[test]
    fn parse_pod_uid_cgroupv1() {
        let content = "12:cpuacct,cpu:/kubepods/besteffort/podabc-def-123/container456\n";
        assert_eq!(parse_pod_uid(content).as_deref(), Some("abc-def-123"),);
    }

    #[test]
    fn parse_pod_uid_systemd_slice() {
        let content = "0::/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod1a2b3c4d_5e6f_7890.slice/cri-abc.scope\n";
        assert_eq!(
            parse_pod_uid(content).as_deref(),
            Some("1a2b3c4d-5e6f-7890"),
        );
    }

    #[test]
    fn parse_pod_uid_not_k8s() {
        let content = "0::/user.slice/user-1000.slice\n";
        assert_eq!(parse_pod_uid(content), None);
    }
}
