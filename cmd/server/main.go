package main

import (
	"fmt"
	"net/http"
	"portfolio/cmd/templates"

	"github.com/a-h/templ"
)

func main() {
	// static files
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))
	
	// home route
	http.Handle("/", templ.Handler(templates.Layout("")))

	fmt.Println("Listening on :3000")
	http.ListenAndServe(":3000", nil)
}