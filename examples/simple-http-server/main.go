package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/hello", helloHandler)
	http.HandleFunc("/time", timeHandler)

	port := ":8080"
	fmt.Printf("Starting HTTP server on http://localhost%s\n", port)
	
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
    <title>Simple HTTP Server</title>
</head>
<body>
    <h1>Welcome to Simple HTTP Server</h1>
    <p>Available endpoints:</p>
    <ul>
        <li><a href="/">/ - This home page</a></li>
        <li><a href="/hello">/hello - Greeting message</a></li>
        <li><a href="/time">/time - Current server time</a></li>
    </ul>
</body>
</html>
`)
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "World"
	}
	fmt.Fprintf(w, "Hello, %s!", name)
}

func timeHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Current server time: %s", time.Now().Format("2006-01-02 15:04:05"))
}