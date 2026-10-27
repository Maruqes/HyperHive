package integration

import "testing"

func TestDuplicateNames(t *testing.T) {
	f := setup(t)
	f.request("POST", "/tabs", `{"name":" general "}`, 409)
	f.request("PUT", "/tabs/general", `{"name":"General"}`, 200)
	f.request("POST", "/apps", `{"name":" EXAMPLE ","tab_id":"general"}`, 409)
	f.request("POST", "/apps/"+f.id+"/replicate", `{"name":"example"}`, 409)
	f.request("POST", "/apps", `{"name":"Other","tab_id":"general","tags":["Prod"," prod "]}`, 400)
	f.request("PUT", "/apps/"+f.id, `{"name":"Example","tab_id":"general","tags":["Go"],"heartbeat_timeout_seconds":10,"retention_days":7}`, 200)
}
