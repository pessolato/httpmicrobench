package server

import (
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
)

// ListenAndServeRand starts a server which responds with a random amount of bytes.
//
// The size of the response is controlled by the client.
func ListenAndServeRand(addr string) error {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		pathParam := r.URL.Path[1:]
		numBytes, err := strconv.Atoi(pathParam)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "unable to convert requested value %s into a valid amount of bytes", pathParam)
			return
		}

		_, err = io.Copy(w, io.LimitReader(rand.Reader, int64(numBytes)))
		if err != nil {
			log.Println(err)
			return
		}
	})

	return http.ListenAndServe(addr, nil)
}
