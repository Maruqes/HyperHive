package npm

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Maruqes/512SvMan/logger"
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
		logger.Warn("Retrying after error:", err)
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

func DeleteBaseUser(base, email, password string) error {
	//login into new user
	token, err := retry[string](60*time.Second, 2*time.Second, func() (string, error) {
		return Login(base, email, password)
	})
	if err != nil {
		return err
	}

	//delete default admin user
	users, err := GetAllUsers(base, token)
	if err != nil {
		return err
	}
	for _, u := range users {
		if u.Email == adminEmail {
			err := DeleteUser(base, token, u.ID)
			if err != nil {
				return err
			}
			logger.Info("Deleted default admin user", adminEmail)
		}
	}
	return nil
}
func SetupNPM(base string) error {

	logger.Info("Pulling and starting NPM container…")
	err := PullImage()
	if err != nil {
		return err
	}

	err = waitForNPM(base, 2*time.Minute)
	if err != nil {
		return err
	}
	logger.Info("NPM is ready at", base)

	// ensure API is ready before we try to use it
	err = waitForAPI(base, 1*time.Minute)
	if err != nil {
		return err
	}
	logger.Info("NPM API is ready…")

	token, err := retry[string](30*time.Second, 2*time.Second, func() (string, error) {
		return Login(base, adminEmail, adminPass)
	})
	if err != nil {
		if strings.Contains(err.Error(), "Invalid email or password") {
			logger.Info("Admin user already changed password, skipping creation.")
			return nil
		}
		return err
	}

	if token != "" {
		//ask for a new user
		fmt.Print("Enter new user email: ")
		var email string
		fmt.Scanln(&email)

		fmt.Print("Enter new user name: ")
		var name string
		fmt.Scanln(&name)

		fmt.Print("Enter new user nick (username): ")
		var nick string
		fmt.Scanln(&nick)

		fmt.Print("Enter new user password: ")
		var pass string
		fmt.Scanln(&pass)

		id, err := CreateUser(base, token, NewUser{
			User: UserCreation{
				Name:       name,
				Nickname:   nick,
				Email:      email,
				Roles:      []string{"admin"},
				IsDisabled: false,
			},
			Password: pass,
		})
		if err != nil {
			return err
		}
		logger.Info("Created new user with id:", id)
		//disable admin user
		err = DeleteBaseUser(base, email, pass)
		if err != nil {
			return err
		}
	}

	return nil
}

func MakeRequest(method, url, token string, body io.Reader, timeoutSeconds int) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
