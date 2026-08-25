package handlers

import "net/http"

func HandleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Dashboard"))
}
