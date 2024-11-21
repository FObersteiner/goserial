//go:build linux
// +build linux

package goserial_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	serial "github.com/FObersteiner/goserial"
)

func TestConnection(t *testing.T) {
	port0 := os.Getenv("PORT0")
	port1 := os.Getenv("PORT1")
	if port0 == "" || port1 == "" {
		t.Skip("skipping test because PORT0 and/or PORT1 environment variable is not set")
	}

	c0 := &serial.Config{Name: port0, Baud: 115200, ReadTimeout: time.Duration(time.Second)}
	c1 := &serial.Config{Name: port1, Baud: 115200, ReadTimeout: time.Duration(time.Second)}

	s1, err := serial.OpenPort(c0)
	if err != nil {
		t.Fatal(err)
	}
	defer s1.Close()

	s2, err := serial.OpenPort(c1)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	readChan := make(chan int, 1)
	errChan := make(chan error, 1)

	go func() {
		buf := make([]byte, 128)
		var readCount int
		for {
			n, err := s2.Read(buf)
			if err != nil {
				errChan <- err
				close(readChan)
				return
			}
			readCount++
			t.Logf("read %v %v bytes: % 02x %s", readCount, n, buf[:n], buf[:n])
			select {
			case <-readChan:
				readChan <- readCount
				close(readChan)
			default:
			}
		}
	}()

	if _, err = s1.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err = s1.Write([]byte(" ")); err != nil {
		t.Fatal(err)
	}

	// setting a time delay here to simulate a subsequent write,
	// causing a second read
	time.Sleep(time.Millisecond * 10)
	if _, err = s1.Write([]byte("world")); err != nil {
		t.Fatal(err)
	}

	readChan <- 0
	_, err = s1.Write([]byte(" ")) // We could be blocked by s2.Read(buf) ...
	if err != nil {
		t.Fatalf("error on write to serial port 1; %v", err)
	}

	// TODO : readChan might be closed; make use of this fact ?
	c := <-readChan
	exp := 5
	if c >= exp {
		t.Fatalf("expected less than %v read, got %v", exp, c)
	}

	err = <-errChan
	if err != io.EOF {
		t.Fatal(err)
	}
}

func TestConnectionLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		// there is a port of socat to Windows, but it seems to rely on cygwin
		// not sure about Mac
		t.Skip("skipping socat test because not on Linux")
	}
	_, err := exec.LookPath("socat")
	if err != nil {
		t.Skip("skipping test because socat was not found in PATH")
	}

	// timeout is a fallback here, if something fails
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	wd, _ := os.Getwd()

	cmd := "socat"
	args := []string{"pty,raw,echo=0,link=/tmp/pty0", "pty,raw,echo=0,link=/tmp/pty1"}

	p, err := startCmd(ctx, wd, cmd, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := p.Signal(os.Interrupt)
		if err != nil {
			t.Fatal(err)
		}
	}()

	// cmd.Start() / starting socat creates a race !
	// (FO) on my machine, 10 ms is a safe time to wait; might be different
	// on your machine.
	time.Sleep(time.Millisecond * 10)

	port0 := &serial.Config{
		Name: "/tmp/pty0",
		// Baud:        115200,
		Baud:        100000,
		ReadTimeout: time.Duration(time.Second),
		Size:        8,
	}
	port1 := &serial.Config{
		Name: "/tmp/pty1",
		// Baud:        115200,
		Baud:        100000,
		ReadTimeout: time.Duration(time.Second),
		Size:        8,
	}

	stream0, err := serial.OpenPort(port0)
	if err != nil {
		t.Fatal("could not setup connection", err)
	}
	stream1, err := serial.OpenPort(port1)
	if err != nil {
		t.Fatal("could not setup connection", err)
	}

	want := []byte("Hello, World!")
	nIn, err := stream1.Write(want)
	if err != nil {
		t.Fatal(err)
	}

	// stream is buffered, so we can read in sequence:
	buf := make([]byte, 1024)
	nOut, err := stream0.Read(buf)
	if err != nil {
		t.Fatal(err)
	}

	if nOut != nIn {
		t.Fatalf("sent %v bytes, got %v", nIn, nOut)
	}

	have := buf[:nOut]
	if !testEq(have, want) {
		t.Fatal("read data does not match written data")
	}

	// after flushing a serial interface, no bytes should be left:
	_, err = stream1.Write(want)
	if err != nil {
		t.Fatal(err)
	}
	// this again is a data race; need to wait a bit after writing before
	// we can flush the bytes...
	time.Sleep(time.Millisecond * 10)
	stream0.Flush()

	nOut, err = stream0.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if nOut != 0 {
		t.Logf("expected zero bytes read after flush, got %v", nOut)
		t.Fail()
	}

	cancel()
}

func TestFindSerial(t *testing.T) {
	_, err := serial.FindSerial()
	if err != nil {
		t.Fatalf("error discovering serial ports; %v", err)
	}
}

// --- HELPERS ------------------------------------------------------------------------

func startCmd(ctx context.Context, wd, cmd string, args ...string) (*os.Process, error) {
	ecmd := exec.CommandContext(ctx, cmd, args...)
	ecmd.Dir = wd
	err := ecmd.Start()
	if err != nil {
		return nil, err
	}
	return ecmd.Process, nil
}

func testEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
