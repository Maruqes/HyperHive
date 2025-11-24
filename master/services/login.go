package services

import "512SvMan/npm"

type LoginService struct {}

func (s *LoginService) Login(baseUrl, email, password string) (string, error) {
	return npm.Login(baseUrl, email, password)
}

func (s *LoginService) IsLoginValid(baseUrl, token string) bool {
	_, err := npm.GetAllUsers(baseUrl, token)
	return err == nil
}
