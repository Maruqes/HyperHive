package hyperhivemcp

import "encoding/json"

var nginxCases []nginxCase

func init() {
	if err := json.Unmarshal([]byte(`[
  {
    "name": "list_certificates",
    "method": "GET",
    "path": "/certs/list",
    "kind": "list",
    "args": {}
  },
  {
    "name": "list_dns_providers",
    "method": "GET",
    "path": "/certs/dns-providers",
    "kind": "list",
    "args": {}
  },
  {
    "name": "download_certificate",
    "method": "POST",
    "path": "/certs/download",
    "kind": "download",
    "args": {
      "id": 42
    }
  },
  {
    "name": "renew_certificate",
    "method": "POST",
    "path": "/certs/renew",
    "kind": "mutation",
    "args": {
      "id": 42
    }
  },
  {
    "name": "list_access_lists",
    "method": "GET",
    "path": "/access-lists/list",
    "kind": "list",
    "args": {}
  },
  {
    "name": "create_access_list",
    "method": "POST",
    "path": "/access-lists/create",
    "kind": "mutation",
    "args": {
      "name": "office",
      "satisfy_any": false,
      "pass_auth": false,
      "items": [
        {
          "username": "fixture-user",
          "password": "fixture-password"
        }
      ],
      "clients": [
        {
          "directive": "allow",
          "address": "10.0.0.0/8"
        },
        {
          "directive": "deny",
          "address": "all"
        }
      ]
    }
  },
  {
    "name": "list_streams",
    "method": "GET",
    "path": "/stream/list",
    "kind": "list",
    "args": {}
  },
  {
    "name": "create_stream",
    "method": "POST",
    "path": "/stream/create",
    "kind": "mutation",
    "args": {
      "incoming_port": 6060,
      "forwarding_host": "upstream.test",
      "forwarding_port": 60,
      "tcp_forwarding": true,
      "udp_forwarding": false,
      "certificate_id": 0,
      "meta": {
        "dns_provider_credentials": "",
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "description": ""
    }
  },
  {
    "name": "update_stream",
    "method": "PUT",
    "path": "/stream/edit",
    "kind": "mutation",
    "args": {
      "incoming_port": 6060,
      "forwarding_host": "upstream.test",
      "forwarding_port": 60,
      "tcp_forwarding": true,
      "udp_forwarding": false,
      "certificate_id": 0,
      "meta": {
        "dns_provider_credentials": "",
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "description": "",
      "id": 42
    }
  },
  {
    "name": "enable_stream",
    "method": "POST",
    "path": "/stream/enable",
    "kind": "mutation",
    "args": {
      "id": 42
    }
  },
  {
    "name": "disable_stream",
    "method": "POST",
    "path": "/stream/disable",
    "kind": "mutation",
    "args": {
      "id": 42
    }
  },
  {
    "name": "list_redirects",
    "method": "GET",
    "path": "/redirection/list",
    "kind": "list",
    "args": {}
  },
  {
    "name": "create_redirect",
    "method": "POST",
    "path": "/redirection/create",
    "kind": "mutation",
    "args": {
      "domain_names": [
        "redirect.test"
      ],
      "forward_scheme": "https",
      "forward_domain_name": "target.test",
      "forward_http_code": "307",
      "preserve_path": false,
      "block_exploits": false,
      "certificate_id": 0,
      "meta": {
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "advanced_config": "",
      "http2_support": false,
      "hsts_enabled": false,
      "hsts_subdomains": false,
      "ssl_forced": false
    }
  },
  {
    "name": "update_redirect",
    "method": "PUT",
    "path": "/redirection/edit",
    "kind": "mutation",
    "args": {
      "domain_names": [
        "redirect.test"
      ],
      "forward_scheme": "https",
      "forward_domain_name": "target.test",
      "forward_http_code": "307",
      "preserve_path": false,
      "block_exploits": false,
      "certificate_id": 0,
      "meta": {
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "advanced_config": "",
      "http2_support": false,
      "hsts_enabled": false,
      "hsts_subdomains": false,
      "ssl_forced": false,
      "id": 42
    }
  },
  {
    "name": "enable_redirect",
    "method": "POST",
    "path": "/redirection/enable",
    "kind": "mutation",
    "args": {
      "id": 42
    }
  },
  {
    "name": "disable_redirect",
    "method": "POST",
    "path": "/redirection/disable",
    "kind": "mutation",
    "args": {
      "id": 42
    }
  },
  {
    "name": "list_404_hosts",
    "method": "GET",
    "path": "/404/list",
    "kind": "list",
    "args": {}
  },
  {
    "name": "create_404_host",
    "method": "POST",
    "path": "/404/create",
    "kind": "mutation",
    "args": {
      "domain_names": [
        "missing.test"
      ],
      "certificate_id": 0,
      "meta": {
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "advanced_config": "",
      "http2_support": false,
      "hsts_enabled": false,
      "hsts_subdomains": false,
      "ssl_forced": false
    }
  },
  {
    "name": "update_404_host",
    "method": "PUT",
    "path": "/404/edit",
    "kind": "mutation",
    "args": {
      "domain_names": [
        "missing.test"
      ],
      "certificate_id": 0,
      "meta": {
        "letsencrypt_agree": false,
        "dns_challenge": false
      },
      "advanced_config": "",
      "http2_support": false,
      "hsts_enabled": false,
      "hsts_subdomains": false,
      "ssl_forced": false,
      "id": 42
    }
  },
  {
    "name": "enable_404_host",
    "method": "POST",
    "path": "/404/enable",
    "kind": "mutation",
    "args": {
      "id": 42
    }
  },
  {
    "name": "disable_404_host",
    "method": "POST",
    "path": "/404/disable",
    "kind": "mutation",
    "args": {
      "id": 42
    }
  }
]`), &nginxCases); err != nil {
		panic(err)
	}
}
