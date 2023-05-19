package main

import (
	"encoding/hex"
	"github.com/iamwavecut/tool"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type peer struct {
	ID       string
	IP       string
	Port     string
	LastSeen int64
}

type swarm struct {
	Peers map[string]*peer
	Mutex sync.RWMutex
}

var swarms = make(map[string]*swarm)

func main() {
	if !hostnameResolvesToLocalhost("retracker.local") {
		log.Println(
			"Warning: retracker.local does not resolve to current host interface. " +
				"This may cause problems with local torrent client.",
		)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/announce", announceHandler)
	wg := sync.WaitGroup{}
	for _, listenAddr := range []string{":80", ":8080"} {
		wg.Add(1)
		go func(listenAddr string) {
			defer wg.Done()
			tool.Try((&http.Server{
				Addr:        listenAddr,
				Handler:     mux,
				ReadTimeout: 1 * time.Second,
			}).ListenAndServe(), true)
		}(listenAddr)
	}
	wg.Wait()
}

func announceHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	infoHash := query.Get("info_hash")
	if infoHash == "" {
		http.Error(w, "Missing info_hash", http.StatusBadRequest)
		return
	}

	peerID := query.Get("peer_id")
	if peerID == "" {
		http.Error(w, "Missing peer_id", http.StatusBadRequest)
		return
	}

	port := query.Get("port")
	if port == "" {
		http.Error(w, "Missing port", http.StatusBadRequest)
		return
	}

	ip := r.RemoteAddr
	if query.Get("ip") != "" {
		ip = query.Get("ip")
	}

	host, _, err := net.SplitHostPort(ip)
	if err != nil {
		http.Error(w, "Invalid IP address", http.StatusBadRequest)
		return
	}

	sw, exists := swarms[infoHash]
	if !exists {
		sw = &swarm{Peers: make(map[string]*peer)}
		swarms[infoHash] = sw
	}

	p := &peer{ID: peerID, IP: host, Port: port, LastSeen: time.Now().Unix()}
	sw.Mutex.Lock()
	sw.Peers[peerID] = p
	sw.Mutex.Unlock()
	log.Println("Announced:", hex.EncodeToString([]byte(infoHash)), peerID, host, port)

	go cleanSwarm(infoHash)

	sw.Mutex.RLock()
	peers := make([]string, 0, len(sw.Peers))
	for _, p := range sw.Peers {
		peers = append(peers, net.JoinHostPort(p.IP, p.Port))
	}
	sw.Mutex.RUnlock()

	w.Header().Set("Content-Type", "text/plain")
	for _, p := range peers {
		_ = tool.Err(w.Write([]byte(p + "\n")))
	}
}

func cleanSwarm(infoHash string) {
	sw := swarms[infoHash]
	sw.Mutex.Lock()
	defer sw.Mutex.Unlock()

	now := time.Now().Unix()
	for id, p := range sw.Peers {
		if now-p.LastSeen > 1800 {
			delete(sw.Peers, id)
		}
	}
}

func hostnameResolvesToLocalhost(hostname string) bool {
	defer tool.Catch(func(err error) {
		log.Println("Couldn't check the host name:", err)
	})
	addrs := tool.MustReturn(net.LookupIP(hostname))
	localAddrs := tool.MustReturn(net.InterfaceAddrs())
	for _, addr := range addrs {
		for _, localAddr := range localAddrs {
			if localAddr, ok := localAddr.(*net.IPNet); ok && localAddr.Contains(addr) {
				return true
			}
		}
	}
	return false
}
