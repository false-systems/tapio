use std::io::{Read, Write};
use std::net::TcpStream;
use std::time::Duration;

pub const DEFAULT_TIMEOUT: Duration = Duration::from_secs(5);
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub const DEFAULT_MAX_RESPONSE_BYTES: usize = 1024 * 1024;
const POST_MAX_RESPONSE_BYTES: usize = 8 * 1024;

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
#[derive(Debug)]
pub struct HttpResponse {
    pub status: u16,
    pub body: Vec<u8>,
}

#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub fn get(
    endpoint: &str,
    headers: &[(&str, String)],
    timeout: Duration,
    max_response_bytes: usize,
) -> Result<HttpResponse, String> {
    request("GET", endpoint, headers, &[], timeout, max_response_bytes)
}

pub fn post_json(endpoint: &str, body: &[u8]) -> Result<(), String> {
    let response = post_json_response(endpoint, body, POST_MAX_RESPONSE_BYTES)?;

    if (200..300).contains(&response.status) {
        Ok(())
    } else {
        Err(format!("HTTP {}", response.status))
    }
}

pub fn post_json_response(
    endpoint: &str,
    body: &[u8],
    max_response_bytes: usize,
) -> Result<HttpResponse, String> {
    request(
        "POST",
        endpoint,
        &[("Content-Type", "application/json".to_string())],
        body,
        DEFAULT_TIMEOUT,
        max_response_bytes,
    )
}

fn request(
    method: &str,
    endpoint: &str,
    headers: &[(&str, String)],
    body: &[u8],
    timeout: Duration,
    max_response_bytes: usize,
) -> Result<HttpResponse, String> {
    let url = endpoint
        .strip_prefix("http://")
        .ok_or_else(|| format!("requires http://: {endpoint}"))?;

    let (host_port, path) = match url.find('/') {
        Some(i) => (&url[..i], &url[i..]),
        None if method == "POST" => (url, "/v1/occurrences"),
        None => (url, "/"),
    };

    let mut stream = TcpStream::connect(host_port).map_err(|e| format!("connect: {e}"))?;
    stream.set_write_timeout(Some(timeout)).ok();
    stream.set_read_timeout(Some(timeout)).ok();

    let mut request = format!("{method} {path} HTTP/1.1\r\nHost: {host_port}\r\n");
    for (name, value) in headers {
        request.push_str(name);
        request.push_str(": ");
        request.push_str(value);
        request.push_str("\r\n");
    }
    if !body.is_empty() {
        request.push_str(&format!("Content-Length: {}\r\n", body.len()));
    }
    request.push_str("Connection: close\r\n\r\n");

    stream
        .write_all(request.as_bytes())
        .map_err(|e| format!("write: {e}"))?;
    if !body.is_empty() {
        stream
            .write_all(body)
            .map_err(|e| format!("write body: {e}"))?;
    }

    read_response(stream, max_response_bytes)
}

fn read_response(mut stream: TcpStream, max_response_bytes: usize) -> Result<HttpResponse, String> {
    let mut response = Vec::new();
    let mut chunk = [0u8; 4096];
    loop {
        let n = match stream.read(&mut chunk) {
            Ok(n) => n,
            Err(e) if e.kind() == std::io::ErrorKind::ConnectionReset && !response.is_empty() => {
                break;
            }
            Err(e) => return Err(format!("read: {e}")),
        };
        if n == 0 {
            break;
        }
        response.extend_from_slice(&chunk[..n]);
        if response.len() > max_response_bytes {
            return Err(format!("response exceeded {max_response_bytes} bytes"));
        }
    }

    let split = response
        .windows(4)
        .position(|window| window == b"\r\n\r\n")
        .ok_or_else(|| "bad HTTP response".to_string())?;
    let (head, body_with_sep) = response.split_at(split);
    let body = body_with_sep[4..].to_vec();
    let head = std::str::from_utf8(head).map_err(|e| format!("headers utf8: {e}"))?;
    let mut lines = head.lines();
    let status_line = lines
        .next()
        .ok_or_else(|| "missing HTTP status".to_string())?;
    let status = parse_status(status_line)?;
    Ok(HttpResponse { status, body })
}

fn parse_status(status_line: &str) -> Result<u16, String> {
    if !status_line.starts_with("HTTP/1.1 ") && !status_line.starts_with("HTTP/1.0 ") {
        return Err("bad HTTP version".into());
    }
    status_line
        .get(9..12)
        .ok_or_else(|| "bad HTTP status".to_string())?
        .parse::<u16>()
        .map_err(|e| format!("bad HTTP status: {e}"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;
    use std::net::TcpListener;
    use std::thread;

    fn with_server(response: &'static [u8], f: impl FnOnce(String)) {
        with_server_checking_request(response, f, |_| {});
    }

    fn with_server_checking_request(
        response: &'static [u8],
        f: impl FnOnce(String),
        check_request: impl FnOnce(&str) + Send + 'static,
    ) {
        let listener = match TcpListener::bind("127.0.0.1:0") {
            Ok(listener) => listener,
            Err(e)
                if e.kind() == std::io::ErrorKind::PermissionDenied
                    && std::env::var_os("TAPIO_LEAN_REQUIRE_NET").is_none() =>
            {
                eprintln!("SKIP httpc loopback test: loopback bind not permitted ({e})");
                return;
            }
            Err(e) => panic!("loopback bind failed: {e}"),
        };
        let addr = listener.local_addr().unwrap();
        let handle = thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut request = [0u8; 2048];
            let n = stream.read(&mut request).unwrap();
            check_request(&String::from_utf8_lossy(&request[..n]));
            stream.write_all(response).unwrap();
        });
        f(format!("http://{addr}/v1/agents/config"));
        handle.join().unwrap();
    }

    #[test]
    fn get_parses_status_headers_and_body() {
        with_server(
            b"HTTP/1.1 200 OK\r\nETag: \"sha256:abc\"\r\nContent-Length: 2\r\n\r\n{}",
            |url| {
                let response = get(&url, &[], DEFAULT_TIMEOUT, DEFAULT_MAX_RESPONSE_BYTES).unwrap();
                assert_eq!(response.status, 200);
                assert_eq!(response.body, b"{}");
            },
        );
    }

    #[test]
    fn get_parses_not_modified_without_body() {
        with_server(b"HTTP/1.1 304 Not Modified\r\n\r\n", |url| {
            let response = get(
                &url,
                &[("If-None-Match", "\"sha256:abc\"".to_string())],
                DEFAULT_TIMEOUT,
                DEFAULT_MAX_RESPONSE_BYTES,
            )
            .unwrap();
            assert_eq!(response.status, 304);
            assert!(response.body.is_empty());
        });
    }

    #[test]
    fn get_writes_if_none_match_header() {
        with_server_checking_request(
            b"HTTP/1.1 304 Not Modified\r\n\r\n",
            |url| {
                let response = get(
                    &url,
                    &[("If-None-Match", "\"sha256:abc\"".to_string())],
                    DEFAULT_TIMEOUT,
                    DEFAULT_MAX_RESPONSE_BYTES,
                )
                .unwrap();
                assert_eq!(response.status, 304);
            },
            |request| assert!(request.contains("\r\nIf-None-Match: \"sha256:abc\"\r\n")),
        );
    }

    #[test]
    fn oversized_response_is_rejected() {
        with_server(b"HTTP/1.1 200 OK\r\n\r\nabcdef", |url| {
            let err = match get(&url, &[], DEFAULT_TIMEOUT, 3) {
                Ok(_) => panic!("oversized response should fail"),
                Err(err) => err,
            };
            assert!(err.contains("exceeded"));
        });
    }

    #[test]
    fn post_json_tolerates_verbose_2xx_response() {
        with_server(
            b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nX-Padding: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\r\nContent-Length: 64\r\n\r\n{\"accepted\":256,\"rejected\":0,\"next_config_version\":\"1\",\"x\":\"y\"}",
            |url| post_json(&url, b"{}").unwrap(),
        );
    }

    #[test]
    fn post_json_response_returns_body() {
        with_server(
            b"HTTP/1.1 202 Accepted\r\nContent-Length: 2\r\n\r\n{}",
            |url| {
                let response = post_json_response(&url, b"{}", DEFAULT_MAX_RESPONSE_BYTES).unwrap();
                assert_eq!(response.status, 202);
                assert_eq!(response.body, b"{}");
            },
        );
    }

    #[test]
    fn post_json_response_rejects_https_endpoint() {
        let err = post_json_response("https://127.0.0.1:4318", b"{}", DEFAULT_MAX_RESPONSE_BYTES)
            .unwrap_err();
        assert!(err.contains("http://"));
    }
}
