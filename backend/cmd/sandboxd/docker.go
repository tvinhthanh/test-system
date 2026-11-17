package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// docker is the few Docker Engine API calls sandboxd makes, over the
// daemon's socket (DOCKER_HOST: unix:///var/run/docker.sock, or tcp://
// in tests). Nothing else in ATP talks to Docker.
type docker struct {
	host string // unix:///path or tcp://host:port
	http *http.Client
}

// apiVersion: Docker 20.10 and later.
const apiVersion = "/v1.41"

var errNoSuch = errors.New("docker: no such object")

func newDocker(host string) (*docker, error) {
	u, err := url.Parse(host)
	if err != nil || (u.Scheme != "unix" && u.Scheme != "tcp") {
		return nil, fmt.Errorf("DOCKER_HOST=%q: want unix:///path or tcp://host:port", host)
	}
	d := &docker{host: host}
	d.http = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return d.dial(ctx)
	}}}
	return d, nil
}

func (d *docker) dial(ctx context.Context) (net.Conn, error) {
	u, _ := url.Parse(d.host)
	var dl net.Dialer
	if u.Scheme == "unix" {
		return dl.DialContext(ctx, "unix", u.Path)
	}
	return dl.DialContext(ctx, "tcp", u.Host)
}

// call sends one request; out (when not nil) receives the JSON answer.
func (d *docker) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+apiVersion+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("%w: %s", errNoSuch, dockerMessage(msg))
	}
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotModified {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("docker %s %s: %d %s", method, path, resp.StatusCode, dockerMessage(msg))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out)
}

func dockerMessage(b []byte) string {
	var m struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &m) == nil && m.Message != "" {
		return m.Message
	}
	return strings.TrimSpace(string(b))
}

// ------------------------------------------------------------- networks

func (d *docker) createNetwork(ctx context.Context, name string, labels map[string]string) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	// Internal: no gateway, no route anywhere but the containers on it.
	err := d.call(ctx, http.MethodPost, "/networks/create", map[string]any{
		"Name": name, "Driver": "bridge", "Internal": true, "CheckDuplicate": true, "Labels": labels,
	}, &out)
	return out.ID, err
}

func (d *docker) connect(ctx context.Context, network, container string) error {
	return d.call(ctx, http.MethodPost, "/networks/"+url.PathEscape(network)+"/connect", map[string]any{"Container": container}, nil)
}

func (d *docker) disconnect(ctx context.Context, network, container string) error {
	return d.call(ctx, http.MethodPost, "/networks/"+url.PathEscape(network)+"/disconnect", map[string]any{"Container": container, "Force": true}, nil)
}

func (d *docker) removeNetwork(ctx context.Context, network string) error {
	return d.call(ctx, http.MethodDelete, "/networks/"+url.PathEscape(network), nil, nil)
}

type networkInfo struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
	IPAM struct {
		Config []struct {
			Subnet string `json:"Subnet"`
		} `json:"Config"`
	} `json:"IPAM"`
	Containers map[string]struct {
		IPv4Address string `json:"IPv4Address"`
	} `json:"Containers"`
}

func (d *docker) inspectNetwork(ctx context.Context, network string) (networkInfo, error) {
	var out networkInfo
	err := d.call(ctx, http.MethodGet, "/networks/"+url.PathEscape(network), nil, &out)
	return out, err
}

func (d *docker) networks(ctx context.Context, label string) ([]networkInfo, error) {
	var out []networkInfo
	f, _ := json.Marshal(map[string][]string{"label": {label}})
	err := d.call(ctx, http.MethodGet, "/networks?filters="+url.QueryEscape(string(f)), nil, &out)
	return out, err
}

// ----------------------------------------------------------- containers

type containerInfo struct {
	ID              string `json:"Id"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

func (d *docker) inspect(ctx context.Context, container string) (containerInfo, error) {
	var out containerInfo
	err := d.call(ctx, http.MethodGet, "/containers/"+url.PathEscape(container)+"/json", nil, &out)
	return out, err
}

func (d *docker) create(ctx context.Context, name string, cfg any) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	err := d.call(ctx, http.MethodPost, "/containers/create?name="+url.QueryEscape(name), cfg, &out)
	return out.ID, err
}

func (d *docker) start(ctx context.Context, id string) error {
	return d.call(ctx, http.MethodPost, "/containers/"+id+"/start", nil, nil)
}

func (d *docker) kill(ctx context.Context, id string) error {
	return d.call(ctx, http.MethodPost, "/containers/"+id+"/kill", nil, nil)
}

func (d *docker) remove(ctx context.Context, id string) error {
	return d.call(ctx, http.MethodDelete, "/containers/"+url.PathEscape(id)+"?force=1&v=1", nil, nil)
}

// wait blocks until the container stops and returns its exit code.
func (d *docker) wait(ctx context.Context, id string) (int, error) {
	var out struct {
		StatusCode int `json:"StatusCode"`
	}
	err := d.call(ctx, http.MethodPost, "/containers/"+id+"/wait", nil, &out)
	return out.StatusCode, err
}

func (d *docker) containers(ctx context.Context, label string) ([]string, error) {
	var out []struct {
		ID string `json:"Id"`
	}
	f, _ := json.Marshal(map[string][]string{"label": {label}})
	if err := d.call(ctx, http.MethodGet, "/containers/json?all=1&filters="+url.QueryEscape(string(f)), nil, &out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out))
	for _, c := range out {
		ids = append(ids, c.ID)
	}
	return ids, nil
}

func (d *docker) imageExists(ctx context.Context, image string) error {
	return d.call(ctx, http.MethodGet, "/images/"+image+"/json", nil, nil)
}

// attachStdin hijacks the container's stdin, as `docker run -i` does: call
// it before start, write the bytes after, then close the writer.
func (d *docker) attachStdin(ctx context.Context, id string) (io.WriteCloser, error) {
	conn, err := d.dial(ctx)
	if err != nil {
		return nil, err
	}
	req := "POST " + apiVersion + "/containers/" + id + "/attach?stream=1&stdin=1 HTTP/1.1\r\n" +
		"Host: docker\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Length: 0\r\n\r\n"
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		conn.Close()
		return nil, fmt.Errorf("docker attach: %d", resp.StatusCode)
	}
	_ = conn.SetDeadline(time.Time{})
	return halfCloser{conn}, nil
}

// halfCloser closes the write side only, so the daemon sees EOF on stdin.
type halfCloser struct{ net.Conn }

func (h halfCloser) Close() error {
	if cw, ok := h.Conn.(interface{ CloseWrite() error }); ok {
		err := cw.CloseWrite()
		go func() { // drain, then let go
			_, _ = io.Copy(io.Discard, io.LimitReader(h.Conn, 1<<20))
			h.Conn.Close()
		}()
		return err
	}
	return h.Conn.Close()
}

// logs returns the container's stdout and stderr, each up to its limit
// (the rest is read and dropped; stderr keeps its tail).
func (d *docker) logs(ctx context.Context, id string, maxOut, maxErr int) (stdout, stderr []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+apiVersion+"/containers/"+id+"/logs?stdout=1&stderr=1", nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("docker logs: %d", resp.StatusCode)
	}
	return demux(resp.Body, maxOut, maxErr)
}

// demux splits Docker's multiplexed stream: 8-byte headers [stream, 0, 0,
// 0, size(4, big-endian)], stream 1 stdout, 2 stderr.
func demux(r io.Reader, maxOut, maxErr int) (stdout, stderr []byte, err error) {
	var hdr [8]byte
	var out, errb bytes.Buffer
	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return out.Bytes(), errb.Bytes(), nil
			}
			return out.Bytes(), errb.Bytes(), err
		}
		n := int64(binary.BigEndian.Uint32(hdr[4:]))
		switch hdr[0] {
		case 1:
			room := int64(maxOut - out.Len())
			if room > 0 {
				if _, err := io.CopyN(&out, r, min(n, room)); err != nil {
					return out.Bytes(), errb.Bytes(), err
				}
				n -= min(n, room)
			}
		case 2:
			if _, err := io.CopyN(&errb, r, n); err != nil {
				return out.Bytes(), errb.Bytes(), err
			}
			n = 0
			if errb.Len() > 2*maxErr { // keep the tail
				tail := append([]byte(nil), errb.Bytes()[errb.Len()-maxErr:]...)
				errb.Reset()
				errb.Write(tail)
			}
		}
		if n > 0 {
			if _, err := io.CopyN(io.Discard, r, n); err != nil {
				return out.Bytes(), errb.Bytes(), err
			}
		}
	}
}
