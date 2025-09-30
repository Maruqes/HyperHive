package npm

import (
	"512SvMan/env512"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	image      = "jc21/nginx-proxy-manager:latest"
	container  = "npm-from-go"
	hostHTTP   = "127.0.0.1:80"      // -> container 80
	hostAdmin  = "127.0.0.1:81"      // -> container 81 (API/UI)
	hostHTTPS  = "127.0.0.1:443"     // -> container 443
	adminEmail = "admin@example.com" // change if you set INITIAL_ADMIN_EMAIL
	adminPass  = "changeme"          // change if you set INITIAL_ADMIN_PASSWORD

)

func waitForNPM(baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}

	// try a few endpoints that come up at slightly different times
	endpoints := []string{
		"/api/schema", // shows up a bit later on some versions
		"/api",        // generic
		"/",           // UI root (often 200/302 before /api)
	}

	for time.Now().Before(deadline) {
		for _, ep := range endpoints {
			resp, err := client.Get(baseURL + ep)
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				// 200–499 is “server up enough to answer”; 5xx means still starting
				if resp.StatusCode < 500 {
					return nil
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("npm not ready at %s within %s", baseURL, timeout)
}

func waitForAPI(baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}
	// endpoints that prove the backend (Node) is answering (401/404 is OK)
	checks := []string{"/api", "/api/schema"}

	for time.Now().Before(deadline) {
		for _, ep := range checks {
			resp, err := client.Get(baseURL + ep)
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				// treat any non-5xx as "backend is reachable"
				if resp.StatusCode < 500 {
					return nil
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("API not ready at %s within %s", baseURL, timeout)
}

func retry[T any](timeout, step time.Duration, fn func() (T, error)) (T, error) {
	var zero T
	deadline := time.Now().Add(timeout)
	for {
		val, err := fn()
		if err == nil {
			return val, nil
		}
		// retry on transient/5xx/connect errors
		if time.Now().After(deadline) {
			return zero, err
		}
		time.Sleep(step)
		fmt.Println("Retrying after error:", err)
	}
}

func PullImage() error {
	// 1) Ensure data dirs
	work, err := os.Getwd()
	if err != nil {
		return err
	}

	data := filepath.Join(work, "npm-data")
	ssl := filepath.Join(work, "npm-ssl")
	if err := os.MkdirAll(data, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(ssl, 0o755); err != nil {
		return err
	}

	// 2) Write docker-compose.yml
	composeFile := filepath.Join(work, "docker-compose.yml")
	composeContent := fmt.Sprintf(`version: "3"
services:
  app:
    image: jc21/nginx-proxy-manager:latest
    restart: unless-stopped
    network_mode: "host"
    ports:
      - "80:80"
      - "443:443"
      - "81:81"
    volumes:
      - %s:/data
      - %s:/etc/letsencrypt
`, data, ssl)

	if err := os.WriteFile(composeFile, []byte(composeContent), 0o644); err != nil {
		return err
	}

	// 3) Run docker compose up -d
	cmd := exec.Command("docker", "compose", "up", "-d")
	cmd.Dir = work // run in project directory
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func SetupNPM(base string) (string, error) {

	fmt.Println("Pulling and starting NPM container…")
	err := PullImage()
	if err != nil {
		return "", err
	}

	err = waitForNPM(base, 2*time.Minute)
	if err != nil {
		return "", err
	}
	fmt.Println("NPM is ready at", base)

	// ensure API is ready before we try to use it
	err = waitForAPI(base, 1*time.Minute)
	if err != nil {
		return "", err
	}
	fmt.Println("NPM API is ready…")

	//try new user first, in case we re-run against existing setup
	token, err := retry[string](60*time.Second, 2*time.Second, func() (string, error) {
		return Login(base, env512.NPM_USER_EMAIL, env512.NPM_USER_PASS)
	})
	if err == nil {
		fmt.Println("New user already exists, logged in as", env512.NPM_USER_NICK)
		return token, nil
	}

	//we failed login as new user, try admin
	token, err = retry[string](60*time.Second, 2*time.Second, func() (string, error) {
		return Login(base, adminEmail, adminPass)
	})
	if err != nil {
		if err != nil {
			return "", err
		}
		return token, nil
	}

	//create new user
	userID, err := CreateUser(base, token, NewUser{
		User: UserCreation{
			Name:     env512.NPM_USER_NAME,
			Nickname: env512.NPM_USER_NICK,
			Email:    env512.NPM_USER_EMAIL,
			Roles:    []string{"admin"},
		},
		Password: env512.NPM_USER_PASS,
	})
	if err != nil {
		return "", err
	}
	fmt.Println("Created user", env512.NPM_USER_NICK+" with ID "+fmt.Sprint(userID))

	//login into new user
	token, err = retry[string](60*time.Second, 2*time.Second, func() (string, error) {
		return Login(base, env512.NPM_USER_EMAIL, env512.NPM_USER_PASS)
	})
	if err != nil {
		return "", err
	}

	//delete default admin user
	users, err := GetAllUsers(base, token)
	if err != nil {
		return "", err
	}
	for _, u := range users {
		if u.Email == adminEmail {
			err := DeleteUser(base, token, u.ID)
			if err != nil {
				return "", err
			}
			fmt.Println("Deleted default admin user", adminEmail)
		}
	}

	return token, nil
}
