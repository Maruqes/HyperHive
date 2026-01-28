package npm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// /api/nginx/access-lists
/*
{
  "name":"name",
  "satisfy_any":false,
  "pass_auth":false,
  "items":[{"username":"user","password":"pass"}],
  "clients":[{"directive":"allow","address":"192.168.1.198"}]
}
*/

type AccessList struct {
	ID          int               `json:"id"`
	CreatedOn   string            `json:"created_on,omitempty"`
	ModifiedOn  string            `json:"modified_on,omitempty"`
	Name        string            `json:"name"`
	SatisfyAny  bool              `json:"satisfy_any"`
	PassAuth    bool              `json:"pass_auth"`
	OwnerUserID int               `json:"owner_user_id,omitempty"`
	Items       []AccessListItem  `json:"items"`
	Clients     []AccessListEntry `json:"clients"`
	Meta        interface{}       `json:"meta,omitempty"`
}

type AccessListItem struct {
	ID           int    `json:"id,omitempty"`
	CreatedOn    string `json:"created_on,omitempty"`
	ModifiedOn   string `json:"modified_on,omitempty"`
	Username     string `json:"username"`
	Password     string `json:"password,omitempty"`
	AccessListID int    `json:"access_list_id,omitempty"`
}

type AccessListEntry struct {
	ID           int    `json:"id,omitempty"`
	CreatedOn    string `json:"created_on,omitempty"`
	ModifiedOn   string `json:"modified_on,omitempty"`
	Directive    string `json:"directive"`
	Address      string `json:"address"`
	AccessListID int    `json:"access_list_id,omitempty"`
}

// POST /api/nginx/access-lists
func CreateAccessList(baseURL, token string, list AccessList) (int, error) {
	reqBody := map[string]any{
		"name":        list.Name,
		"satisfy_any": list.SatisfyAny,
		"pass_auth":   list.PassAuth,
		"items":       list.Items,
		"clients":     list.Clients,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return 0, err
	}

	resp, err := MakeRequest("POST", baseURL+"/api/nginx/access-lists", token, bytes.NewReader(jsonData), 30)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return 0, fmt.Errorf("create access list failed (%d): %s", resp.StatusCode, respBody)
	}

	id := -1
	var respData map[string]any
	if err := json.Unmarshal(respBody, &respData); err == nil {
		if d, ok := respData["id"].(float64); ok {
			id = int(d)
		}
	}
	return id, nil
}

// PUT /api/nginx/access-lists/{id}
func EditAccessList(baseURL, token string, list AccessList) error {
	reqBody := map[string]any{
		"name":        list.Name,
		"satisfy_any": list.SatisfyAny,
		"pass_auth":   list.PassAuth,
		"items":       list.Items,
		"clients":     list.Clients,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	resp, err := MakeRequest("PUT", fmt.Sprintf("%s/api/nginx/access-lists/%d", baseURL, list.ID), token, bytes.NewReader(jsonData), 30)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("edit access list failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

// DELETE /api/nginx/access-lists/{id}
func DeleteAccessList(baseURL, token string, id int) error {
	resp, err := MakeRequest("DELETE", fmt.Sprintf("%s/api/nginx/access-lists/%d", baseURL, id), token, nil, 30)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 && resp.StatusCode != 204 {
		return fmt.Errorf("delete access list failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

// GET /api/nginx/access-lists (raw response passthrough)
func ListAccessListsRaw(baseURL, token string) ([]byte, error) {
	resp, err := MakeRequest("GET", baseURL+"/api/nginx/access-lists", token, nil, 30)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("list access lists failed (%d): %s", resp.StatusCode, respBody)
	}

	return respBody, nil
}

// GET /api/nginx/access-lists/{id}
func GetAccessList(baseURL, token string, id int) (AccessList, error) {
	resp, err := MakeRequest("GET", fmt.Sprintf("%s/api/nginx/access-lists/%d", baseURL, id), token, nil, 30)
	if err != nil {
		return AccessList{}, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return AccessList{}, fmt.Errorf("get access list failed (%d): %s", resp.StatusCode, respBody)
	}

	var list AccessList
	if err := json.Unmarshal(respBody, &list); err != nil {
		return AccessList{}, err
	}

	return list, nil
}

// GET /api/nginx/access-lists
func ListAccessLists(baseURL, token string) ([]AccessList, error) {
	resp, err := MakeRequest("GET", baseURL+"/api/nginx/access-lists", token, nil, 30)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("list access lists failed (%d): %s", resp.StatusCode, respBody)
	}

	var lists []AccessList
	if err := json.Unmarshal(respBody, &lists); err != nil {
		return nil, err
	}

	return lists, nil
}
