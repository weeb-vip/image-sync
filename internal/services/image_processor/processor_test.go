package image_processor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ThatCatDev/ep/v2/event"
	"github.com/weeb-vip/image-sync/internal/services/storage"
)

// An in-memory bucket with the metadata the real one keeps.
type memStore struct {
	mu   sync.Mutex
	objs map[string]struct {
		data []byte
		ct   string
		meta map[string]string
	}
}

func newMemStore() *memStore {
	return &memStore{objs: map[string]struct {
		data []byte
		ct   string
		meta map[string]string
	}{}}
}

func (m *memStore) Put(ctx context.Context, data []byte, path string) error {
	return m.PutObject(ctx, data, path, "application/octet-stream", nil)
}
func (m *memStore) PutObject(_ context.Context, data []byte, path, ct string, meta map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[path] = struct {
		data []byte
		ct   string
		meta map[string]string
	}{data, ct, meta}
	return nil
}
func (m *memStore) Get(_ context.Context, path string) ([]byte, error) { return m.objs[path].data, nil }
func (m *memStore) Delete(_ context.Context, path string) error        { delete(m.objs, path); return nil }
func (m *memStore) List(context.Context, string, bool) <-chan storage.Entry {
	ch := make(chan storage.Entry)
	close(ch)
	return ch
}
func (m *memStore) Copy(_ context.Context, src, dst string) error {
	m.objs[dst] = m.objs[src]
	return nil
}
func (m *memStore) Exists(_ context.Context, path string) (bool, error) {
	_, ok := m.objs[path]
	return ok, nil
}
func (m *memStore) Stat(_ context.Context, path string) (int64, bool, error) {
	o, ok := m.objs[path]
	return int64(len(o.data)), ok, nil
}
func (m *memStore) Head(_ context.Context, path string) (storage.Info, bool, error) {
	o, ok := m.objs[path]
	return storage.Info{Size: int64(len(o.data)), Meta: o.meta}, ok, nil
}

// A source that serves one JPEG-looking body and counts the GETs it answers.
func source(t *testing.T, body []byte) (*httptest.Server, *int) {
	t.Helper()
	gets := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodGet {
			gets++
			w.Write(body)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &gets
}

var jpeg = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 300)...)

func run(t *testing.T, p ImageProcessor[string], url string) {
	t.Helper()
	ev := event.Event[string, Payload]{Payload: Payload{Data: ImageSchema{ID: "id-1", URL: url, Type: DataTypeAnime}}}
	if _, err := p.Process(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
}

func TestStoresWithTheRealContentTypeAndTheSourceLength(t *testing.T) {
	store := newMemStore()
	srv, gets := source(t, jpeg)
	var published []StoredEvent
	p := NewImageProcessor[string](store, func(_ context.Context, b []byte) error {
		var ev StoredEvent
		json.Unmarshal(b, &ev)
		published = append(published, ev)
		return nil
	})

	run(t, p, srv.URL+"/x.jpg")

	o := store.objs["/id-1"]
	if o.ct != "image/jpeg" {
		t.Errorf("content type %q, want image/jpeg (it used to be octet-stream)", o.ct)
	}
	if o.meta[storage.MetaSourceLength] != strconv.Itoa(len(jpeg)) {
		t.Errorf("source length meta %q", o.meta[storage.MetaSourceLength])
	}
	if *gets != 1 {
		t.Errorf("downloads %d, want 1", *gets)
	}
	if len(published) != 1 || published[0].Path != "/id-1" || published[0].Type != DataTypeAnime || published[0].Size != len(jpeg) {
		t.Errorf("announcement %+v", published)
	}
}

// The regression this exists for: an upscaled object is bigger than its
// source. Judged by its own size it looked changed and was overwritten with
// the small original on the next event; judged by the recorded source
// length it is left alone.
func TestAnObjectRewrittenByTheUpscalerIsNotOverwritten(t *testing.T) {
	store := newMemStore()
	srv, gets := source(t, jpeg)
	announced := 0
	p := NewImageProcessor[string](store, func(context.Context, []byte) error { announced++; return nil })
	run(t, p, srv.URL+"/x.jpg")

	// The upscaler replaces the bytes, keeping the metadata.
	o := store.objs["/id-1"]
	big := make([]byte, 5000)
	store.PutObject(context.Background(), big, "/id-1", "image/jpeg", o.meta)

	run(t, p, srv.URL+"/x.jpg")

	if *gets != 1 {
		t.Fatalf("the second event re-downloaded (%d GETs)", *gets)
	}
	if len(store.objs["/id-1"].data) != 5000 {
		t.Fatal("the upscaled object was overwritten")
	}
	if announced != 1 {
		t.Errorf("announced %d times, want 1 (nothing new was stored)", announced)
	}
}

func TestAGenuinelyNewSourceIsFetchedAgain(t *testing.T) {
	store := newMemStore()
	srv, gets := source(t, jpeg)
	p := NewImageProcessor[string](store, nil)
	run(t, p, srv.URL+"/x.jpg")

	bigger := append(jpeg, make([]byte, 100)...)
	srv2, gets2 := source(t, bigger)
	run(t, p, srv2.URL+"/x.jpg")

	if *gets != 1 || *gets2 != 1 {
		t.Fatalf("GETs %d/%d", *gets, *gets2)
	}
	if len(store.objs["/id-1"].data) != len(bigger) {
		t.Fatal("the new artwork was not stored")
	}
}

// Objects written before the metadata existed: their own size still stands
// in, so an untouched download is still skipped.
func TestLegacyObjectsFallBackToTheirOwnSize(t *testing.T) {
	store := newMemStore()
	srv, gets := source(t, jpeg)
	store.Put(context.Background(), jpeg, "/id-1")
	p := NewImageProcessor[string](store, nil)

	run(t, p, srv.URL+"/x.jpg")

	if *gets != 0 {
		t.Fatalf("re-downloaded a legacy object of the same size (%d GETs)", *gets)
	}
}

func TestAFailedAnnouncementDoesNotFailTheStore(t *testing.T) {
	store := newMemStore()
	srv, _ := source(t, jpeg)
	p := NewImageProcessor[string](store, func(context.Context, []byte) error { return context.DeadlineExceeded })

	run(t, p, srv.URL+"/x.jpg") // run fails the test on an error

	if _, ok := store.objs["/id-1"]; !ok {
		t.Fatal("object not stored")
	}
}

func TestLargerVariantNamesTheLCopyOfAMyAnimeListImage(t *testing.T) {
	cases := map[string]string{
		"https://cdn.myanimelist.net/images/anime/1668/108792.jpg":     "https://cdn.myanimelist.net/images/anime/1668/108792l.jpg",
		"https://cdn.myanimelist.net/images/characters/9/310307.jpg":   "https://cdn.myanimelist.net/images/characters/9/310307l.jpg",
		"https://cdn.myanimelist.net/images/anime/1668/108792l.jpg":    "", // already the large copy
		"https://cdn.myanimelist.net/images/anime/1668/108792.jpg?s=1": "https://cdn.myanimelist.net/images/anime/1668/108792l.jpg?s=1",
		"https://artworks.thetvdb.com/banners/posters/12345.jpg":       "", // not MyAnimeList
		"https://cdn.myanimelist.net/images/questionmark_23.gif":       "",
		"not a url": "",
	}
	for in, want := range cases {
		if got := largerVariant(in); got != want {
			t.Errorf("%s -> %q, want %q", in, got, want)
		}
	}
}

// The large copy is stored when it is there, and its length is what gets
// recorded, so the next event compares against the right source.
func TestTheLargerCopyIsStoredWhenItExists(t *testing.T) {
	large := append(jpeg, make([]byte, 400)...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := jpeg
		if strings.HasSuffix(r.URL.Path, "l.jpg") {
			body = large
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodGet {
			w.Write(body)
		}
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	malHosts[host] = true
	defer delete(malHosts, host)

	store := newMemStore()
	p := NewImageProcessor[string](store, nil)
	run(t, p, srv.URL+"/images/anime/1/2.jpg")

	o := store.objs["/id-1"]
	if len(o.data) != len(large) {
		t.Fatalf("stored %d bytes, want the large copy (%d)", len(o.data), len(large))
	}
	if o.meta[storage.MetaSourceLength] != strconv.Itoa(len(large)) || !strings.HasSuffix(o.meta[storage.MetaSourceURL], "/2l.jpg") {
		t.Errorf("meta %v", o.meta)
	}
}

func TestFallsBackToTheSmallCopyWhenThereIsNoLargeOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "l.jpg") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(jpeg)))
		if r.Method == http.MethodGet {
			w.Write(jpeg)
		}
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	malHosts[host] = true
	defer delete(malHosts, host)

	store := newMemStore()
	run(t, NewImageProcessor[string](store, nil), srv.URL+"/images/anime/1/2.jpg")

	if len(store.objs["/id-1"].data) != len(jpeg) {
		t.Fatal("the small copy should have been stored")
	}
}

// refresh-images: the deliberate re-pull. The bucket already holds exactly
// this source, so Process would skip; Refresh fetches, writes and announces
// anyway, and the object comes back without any upscale provenance.
func TestRefreshStoresAndAnnouncesAnUnchangedSourceAgain(t *testing.T) {
	store := newMemStore()
	srv, gets := source(t, jpeg)
	var published []StoredEvent
	p := NewImageProcessor[string](store, func(_ context.Context, b []byte) error {
		var ev StoredEvent
		json.Unmarshal(b, &ev)
		published = append(published, ev)
		return nil
	})
	src := srv.URL + "/x.jpg"
	run(t, p, src)
	if *gets != 1 || len(published) != 1 {
		t.Fatalf("first store: gets=%d published=%d", *gets, len(published))
	}
	// An upscaler pass marked the object; Process now leaves it alone.
	o := store.objs["/id-1"]
	o.meta["upscaled"] = "x"
	store.objs["/id-1"] = o
	run(t, p, src)
	if *gets != 1 {
		t.Fatalf("Process re-fetched an unchanged source: gets=%d", *gets)
	}

	if err := p.Refresh(context.Background(), ImageSchema{ID: "id-1", URL: src, Type: DataTypeAnime}); err != nil {
		t.Fatal(err)
	}
	if *gets != 2 {
		t.Errorf("Refresh did not fetch: gets=%d", *gets)
	}
	if len(published) != 2 || published[1].Path != "/id-1" {
		t.Errorf("Refresh did not announce: %+v", published)
	}
	if store.objs["/id-1"].meta["upscaled"] != "" {
		t.Error("the refreshed object still carries upscale provenance")
	}
}

func TestRefreshFailsOnASourceThatIsGone(t *testing.T) {
	store := newMemStore()
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	p := NewImageProcessor[string](store, nil)
	err := p.Refresh(context.Background(), ImageSchema{ID: "id-1", URL: srv.URL + "/gone.jpg", Type: DataTypeAnime})
	if err == nil {
		t.Fatal("a 404 source must be an error, not an empty object")
	}
	if _, ok := store.objs["/id-1"]; ok {
		t.Error("the 404 body was stored")
	}
}
