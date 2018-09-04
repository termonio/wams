package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
)

type Section struct {
	Offset    int64
	RawOffset int64
	Id        uint8
	Size      uint64
	Raw       []byte
}

type MemorySection struct {
	memCnt         uint64
	maxPages       uint64
	memPages       uint64
	memPagesOffset int64
	memPagesSize   int
}

func readPreamble(rdr io.ReadSeeker) {
	preamble, err := read(rdr, 8)
	if err != nil {
		log.Fatal(err)
	}
	if !bytes.Equal(preamble, []byte("\x00asm\x01\x00\x00\x00")) {
		log.Fatal("not a wasm (version 1) file")
	}
}

func readSection(rdr io.ReadSeeker) *Section {
	id, err := read(rdr, 1)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		log.Fatal(err)
	}
	off, err := rdr.Seek(0, io.SeekCurrent)
	if err != nil {
		log.Fatal(err)
	}
	size, p := read128UlebSize(rdr)
	raw, err := read(rdr, int(size))
	if err != nil {
		log.Fatal(err)
	}
	return &Section{off - 1, off + int64(p), id[0], size, raw}
}

func parseMemorySection(memSect *Section) *MemorySection {
	var pOff int
	rdr := bytes.NewReader(memSect.Raw)
	memCnt, pOff := read128UlebSize(rdr)
	if memCnt != 1 {
		log.Fatal("there should be just one memory")
	}
	maxMem, p := read128UlebSize(rdr)
	pOff += p
	memPages, p := read128UlebSize(rdr)
	pageOffset := memSect.RawOffset + int64(pOff)
	return &MemorySection{memCnt, maxMem, memPages, pageOffset, p}
}

func getMemorySection(file string) *MemorySection {
	rdr, err := os.Open(file)
	if err != nil {
		log.Fatal(err)
	}
	defer rdr.Close()

	readPreamble(rdr)
	for {
		s := readSection(rdr)
		if s == nil {
			break
		}
		if s.Id == 5 {
			fmt.Printf("memory section (offset 0x%x, %d bytes)\n", s.RawOffset, s.Size)
			memSect := parseMemorySection(s)
			if memSect.maxPages == 0 {
				fmt.Println("    maxPages set to unlimited")
			} else {
				fmt.Printf("    maxPages %d\n", memSect.maxPages)
			}
			fmt.Printf("    memPages %d (offset 0x%x, %d bytes)\n", memSect.memPages, memSect.memPagesOffset, memSect.memPagesSize)

			return memSect
		}
		// fmt.Printf("id: %d, size: %d offset: %x, content offset: %x\n", s.Id, s.Size, s.Offset, s.RawOffset)
	}
	return nil
}

func patchFile(file string, bytes []byte, offset int64) {
	finfo, err := os.Stat(file)
	if err != nil {
		log.Fatal(err)
	}
	w, err := os.OpenFile(file, os.O_WRONLY, finfo.Mode())
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()
	_, err = w.Seek(int64(offset), io.SeekStart)
	if err != nil {
		log.Fatal(err)
	}
	n, err := w.Write(bytes)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d bytes written at 0x%x\n", n, offset)
}

func read128UlebSize(rdr io.Reader) (uint64, int) {
	buf := make([]byte, 1)
	var cnt int
	var v, r uint64
	for {
		if _, err := io.ReadFull(rdr, buf); err != nil {
			log.Fatal(err)
		}
		cnt++
		c := uint64(buf[0])
		r |= (c & 0x7f) << v
		if c&0x80 == 0 {
			break
		}
		v += 7
	}
	return r, cnt
}

func read128Uleb(rdr io.Reader) uint64 {
	r, _ := read128UlebSize(rdr)
	return r
}

func write128UlebFixedSize(v uint64, size int) []byte {
	b := make([]byte, size)
	for i := 0; i < size; i++ {
		c := uint8(v & 0x7f)
		v >>= 7
		if i < size-1 {
			c |= 0x80
		}
		b[i] = c
	}
	if v != 0 {
		log.Fatal("fixed size too small")
	}
	return b
}

func read(rdr io.Reader, n int) ([]byte, error) {
	bytes := make([]byte, n)
	_, err := rdr.Read(bytes)
	return bytes, err
}

func main() {
	pages := flag.Uint64("pages", 2048, "initial pages")
	modify := flag.Bool("write", false, "write modification to file")
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Println("need a wasm file to proceed")
		os.Exit(1)
	}
	file := flag.Arg(0)
	memSect := getMemorySection(file)
	if memSect != nil {
		if *pages != 0 && *modify {
			fmt.Printf("setting memPages to %d\n", *pages)
			patch := write128UlebFixedSize(uint64(*pages), memSect.memPagesSize)
			patchFile(file, patch, int64(memSect.memPagesOffset))
		}
	}
}
