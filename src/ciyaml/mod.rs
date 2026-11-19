use serde_yaml::Value;

pub struct CiYamlPatcher;

impl CiYamlPatcher {
    pub fn find_tags(raw_yaml: &str) -> Vec<(String, Vec<String>)> {
        let value: Result<Value, _> = serde_yaml::from_str(raw_yaml);
        let mut results = Vec::new();

        if let Ok(Value::Mapping(map)) = value {
            for (key, val) in map {
                let job_name = match key.as_str() {
                    Some(s) => s.to_string(),
                    None => continue,
                };

                // Hidden jobs or special keys (.pre, .post, stages, variables, etc.)
                if job_name.starts_with('.') && !job_name.starts_with(".runner-") {
                    // Hidden template jobs can still have tags
                }

                if let Some(tags) = Self::extract_tags_from_job(&val) {
                    if !tags.is_empty() {
                        results.push((job_name, tags));
                    }
                }
            }
        }

        results
    }

    fn extract_tags_from_job(val: &Value) -> Option<Vec<String>> {
        if let Value::Mapping(job_map) = val {
            for (k, v) in job_map {
                if k.as_str() == Some("tags") {
                    if let Value::Sequence(tag_seq) = v {
                        let tags: Vec<String> = tag_seq
                            .iter()
                            .filter_map(|t| t.as_str().map(|s| s.to_string()))
                            .collect();
                        return Some(tags);
                    }
                }
            }
        }
        None
    }

    pub fn replace_tag(raw_yaml: &str, old_tag: &str, new_tag: &str) -> (String, bool) {
        let mut changed = false;
        let mut value: Value = match serde_yaml::from_str(raw_yaml) {
            Ok(v) => v,
            Err(_) => return (raw_yaml.to_string(), false),
        };

        if let Value::Mapping(ref mut map) = value {
            for (_key, val) in map.iter_mut() {
                if let Value::Mapping(ref mut job_map) = val {
                    for (k, v) in job_map.iter_mut() {
                        if k.as_str() == Some("tags") {
                            if let Value::Sequence(ref mut tag_seq) = v {
                                for tag_val in tag_seq.iter_mut() {
                                    if tag_val.as_str() == Some(old_tag) {
                                        *tag_val = Value::String(new_tag.to_string());
                                        changed = true;
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }

        if changed {
            let output = serde_yaml::to_string(&value).unwrap_or_else(|_| raw_yaml.to_string());
            (output, true)
        } else {
            (raw_yaml.to_string(), false)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_find_and_replace_tags() {
        let yaml = r#"
build-job:
  stage: build
  tags:
    - docker
    - legacy-runner
  script:
    - echo "building"
"#;

        let tags = CiYamlPatcher::find_tags(yaml);
        assert_eq!(tags.len(), 1);
        assert_eq!(tags[0].0, "build-job");
        assert_eq!(tags[0].1, vec!["docker", "legacy-runner"]);

        let (updated, changed) = CiYamlPatcher::replace_tag(yaml, "legacy-runner", "modern-runner");
        assert!(changed);
        assert!(updated.contains("modern-runner"));
        assert!(!updated.contains("legacy-runner"));
    }
}
