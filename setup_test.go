package fynetailscale_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/docker/docker/api/types/container"
	v1 "github.com/juanfont/headscale/gen/go/headscale/v1"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"tailscale.com/client/tailscale"
	"tailscale.com/tsnet"
)

func SetupTailscalePreAuthClient(ctx context.Context) (*tailscale.LocalClient, v1.HeadscaleServiceClient, func(), error) {
	tc, sc, cancel, err := SetupTestTailscale(ctx)
	if err != nil {
		return nil, nil, func() {}, err
	}

	success := false
	defer func() {
		if !success {
			cancel()
		}
	}()

	resp, err := sc.CreatePreAuthKey(ctx, &v1.CreatePreAuthKeyRequest{
		User:      "test",
		Ephemeral: true,
	})
	if err != nil {
		return nil, nil, func() {}, err
	}
	if resp == nil {
		return nil, nil, func() {}, fmt.Errorf("response is nil")
	}

	state, err := os.MkdirTemp("", "fynetailscale-test")
	if err != nil {
		return nil, nil, func() {}, err
	}
	defer func() {
		if !success {
			os.RemoveAll(state)
		}
	}()

	server := tsnet.Server{
		Dir:        state,
		AuthKey:    resp.PreAuthKey.Key,
		Ephemeral:  true,
		ControlURL: tc,
	}
	err = server.Start()
	if err != nil {
		return nil, nil, func() {}, err
	}
	fmt.Println("control url:", server.ControlURL)

	r, err := server.LocalClient()
	if err != nil {
		return nil, nil, func() {}, err
	}

	return r, sc, func() {
		os.RemoveAll(state)
		server.Close()
		cancel()
	}, nil
}

func SetupTestTailscale(ctx context.Context) (string, v1.HeadscaleServiceClient, func(), error) {
	req := testcontainers.ContainerRequest{
		Image:        "headscale/headscale:0.22.3",
		Name:         "headscale",
		ExposedPorts: []string{"8080/tcp", "9090/tcp", "50443/tcp"},
		Cmd:          []string{"sh", "-c", "cp /etc/headscale-config.yaml /etc/headscale/config.yaml && touch /var/run/headscale/db.sqlite && headscale serve"},
		WaitingFor:   wait.ForLog("INF listening and serving metrics on: 0.0.0.0:9090"),
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.Tmpfs = map[string]string{
				"/var/run/headscale": "rw",
				"/etc/headscale":     "rw",
			}
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      "testdata/config.yaml",
				ContainerFilePath: "/etc/headscale-config.yaml",
				FileMode:          0o600,
			},
		},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return "", nil, func() {}, err
	}
	success := false
	defer func() {
		if !success {
			container.Terminate(ctx)
		}
	}()

	ip, err := container.Host(ctx)
	if err != nil {
		return "", nil, func() {}, err
	}

	port, err := container.MappedPort(ctx, "8080")
	if err != nil {
		return "", nil, func() {}, err
	}

	controlPort, err := container.MappedPort(ctx, "50443")
	if err != nil {
		return "", nil, func() {}, err
	}

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r, err := container.Logs(ctx)
				if err != nil {
					continue
				}
				b, err := io.ReadAll(r)
				if err != nil {
					continue
				}
				fmt.Println(string(b))
			}
		}
	}()

	fmt.Println("control port:", controlPort.Port())
	conn, err := grpc.DialContext(ctx, "ipv4:"+ip+":"+controlPort.Port(),
		grpc.WithBlock(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true,
		})))
	if err != nil {
		return "", nil, func() {}, err
	}
	defer func() {
		if !success {
			conn.Close()
		}
	}()

	client := v1.NewHeadscaleServiceClient(conn)
	resp, err := client.CreateUser(ctx, &v1.CreateUserRequest{
		Name: "test",
	})
	if err != nil {
		return "", nil, func() {}, err
	}
	if resp.User == nil {
		return "", nil, func() {}, fmt.Errorf("user is nil")
	}

	success = true
	return "http://" + ip + ":" + port.Port(), client, func() {
		conn.Close()
		container.Terminate(ctx)
	}, nil
}
