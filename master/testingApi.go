package main

import (
	"log"
	"net/http"
)

func webServer() {

	http.Handle("/web/", http.StripPrefix("/web/", http.FileServer(http.Dir("./web"))))

	log.Println("Iniciando webserver em :9595")
	if err := http.ListenAndServe(":9595", nil); err != nil {
		log.Fatalf("webserver error: %v", err)
	}
}
