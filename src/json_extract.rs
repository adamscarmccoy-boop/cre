// Nested JSON extraction helper
use serde_json::Value;

pub fn extract_nested_key<'a>(val: &'a Value, path: &[&str]) -> Option<&'a Value> {
    let mut curr = val;
    for key in path {
        curr = curr.get(key)?;
    }
    Some(curr)
}
