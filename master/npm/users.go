package npm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Maruqes/512SvMan/logger"
)

type loginReq struct {
	Identity string `json:"identity"`
	Secret   string `json:"secret"`
}

func Login(baseURL, email, password string) (string, error) {

	// 5) Login → JWT
	if err := waitForNPM(baseURL, 2*time.Minute); err != nil {
		return "", err
	}

	reqBody := loginReq{
		Identity: email,
		Secret:   password,
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", baseURL+"/api/tokens", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return "", fmt.Errorf("login failed (%d): %s", resp.StatusCode, respBody)
	}

	// Flexible parsing: try common token field names
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err == nil {
		if t, ok := parsed["token"].(string); ok && t != "" {
			return t, nil
		}
		if t, ok := parsed["access_token"].(string); ok && t != "" {
			return t, nil
		}
		if t, ok := parsed["accessToken"].(string); ok && t != "" {
			return t, nil
		}
		if data, ok := parsed["data"].(map[string]any); ok {
			if t, ok := data["token"].(string); ok && t != "" {
				return t, nil
			}
			if t, ok := data["access_token"].(string); ok && t != "" {
				return t, nil
			}
		}
	}

	return "", fmt.Errorf("unable to find token in response: %s", respBody)
}

//[
// {"id":1,
// "created_on":"2025-09-23 20:09:19",
// "modified_on":"2025-09-23 20:09:19",
// "is_disabled":false,
// "email":"admin@example.com",
// "name":"Administrator",
// "nickname":"Admin",
// "avatar":"",
// "roles":["admin"]},

type User struct {
	ID         int      `json:"id"`
	CreatedOn  string   `json:"created_on"`
	ModifiedOn string   `json:"modified_on"`
	IsDisabled bool     `json:"is_disabled"`
	Email      string   `json:"email"`
	Name       string   `json:"name"`
	Nickname   string   `json:"nickname"`
	Avatar     string   `json:"avatar"`
	Roles      []string `json:"roles"`
}

type NewUser struct {
	User     UserCreation `json:"user"`
	Password string       `json:"password"`
}

type UserCreation struct {
	Name       string   `json:"name"`
	Nickname   string   `json:"nickname"`
	Email      string   `json:"email"`
	Roles      []string `json:"roles"`
	IsDisabled bool     `json:"is_disabled"`
}

type Roles struct {
	Admin bool `json:"admin"`
}

// /api/users/x/auth
// {"type":"password","secret":"123"}
func ChangePassword(baseURL, token string, userID int, newPassword string) error {
	reqBody := map[string]string{
		"type":   "password",
		"secret": newPassword,
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", fmt.Sprintf("%s/api/users/%d/auth", baseURL, userID), bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("change password failed (%d): %s", resp.StatusCode, respBody)
	}

	logger.Info("Change password response:", string(respBody))
	return nil
}

//post to /api/users
/*
{"name":"t1","nickname":"t1","email":"t1@t1.com","roles":["admin"],"is_disabled":false}
*/
func CreateUser(baseURL, token string, newUser NewUser) (int, error) {
	b, err := json.Marshal(newUser.User)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequest("POST", baseURL+"/api/users", bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return 0, fmt.Errorf("create user failed (%d): %s", resp.StatusCode, respBody)
	}

	//print body
	id := -1
	//get id from body
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err == nil {
		if id_res, ok := parsed["id"].(float64); ok {
			id = int(id_res)
		}
	}
	if id == -1 {
		return 0, fmt.Errorf("unable to find id in response: %s", respBody)
	}

	return id, ChangePassword(baseURL, token, id, newUser.Password)
}

// DELETE /api/users/x
func DeleteUser(baseURL, token string, userID int) error {
	req, err := http.NewRequest("DELETE", fmt.Sprintf("%s/api/users/%d", baseURL, userID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 && resp.StatusCode != 204 {
		return fmt.Errorf("delete user failed (%d): %s", resp.StatusCode, respBody)
	}

	return nil
}

func GetAllUsers(baseURL, token string) ([]User, error) {
	req, err := http.NewRequest("GET", baseURL+"/api/users", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("get users failed (%d): %s", resp.StatusCode, respBody)
	}

	var users []User
	if err := json.Unmarshal(respBody, &users); err != nil {
		return nil, err
	}

	return users, nil
}
