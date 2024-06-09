package group

import (
	. "github.com/smartystreets/goconvey/convey"

	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func Test_GroupCache(t *testing.T) {

	var Store = map[string][]byte{
		"red":   []byte("#FF0000"),
		"green": []byte("#00FF00"),
		"blue":  []byte("#0000FF"),
	}

	// TIL you can't have multiple groupcache instances in the same running code
	ports := []string{"http://127.0.0.1:8080"} //, "http://127.0.0.1:8081", "http://127.0.0.1:8082"}
	f := func(key string) ([]byte, error) {
		v, ok := Store[key]
		if !ok {
			return []byte{}, ItemNotFoundError
		}
		return v, nil
	}

	fp := func(key string) ([]byte, error) {
		var pStore = map[string][]byte{
			"black": []byte("#FFFFFF"),
		}

		v, ok := pStore[key]
		if !ok {
			return []byte{}, ItemNotFoundError
		}
		return v, nil
	}

	fg := func(key string) ([]byte, error) {
		var gStore = map[string][]byte{
			"white": []byte("#000000"),
		}

		v, ok := gStore[key]
		if !ok {
			return []byte{}, ItemNotFoundError
		}
		return v, nil
	}

	port := ports[0]
	conf := Config{
		CacheSize:     128 << 20,
		Name:          "default",
		ListenAddress: port,
		PeerList:      ports,
		//ItemExpiration: 1 * time.Second,
	}

	c, err := NewCache(conf, f)
	if err != nil {
		panic(err)
	}
	defer c.Close()

	conf.Name = "passwd"
	c.Add(conf, fp)
	conf.Name = "group"
	conf.ItemExpiration = 3 * time.Millisecond
	c.Add(conf, fg)

	Convey("When a new GroupCache is created it looks correct", t, func() {

		names := c.Names()
		So(names, ShouldContain, "passwd")
		So(names, ShouldContain, "group")
		So(c.Exists("passwd"), ShouldBeTrue)
		So(c.Exists("group"), ShouldBeTrue)
		So(c.Exists("nope"), ShouldBeFalse)

		Convey("... Asking for existing and non-existing items works as expected.", func() {

			for name, code := range Store {
				color, ok := c.Get("default", name)
				So(ok, ShouldBeTrue)
				So(color, ShouldResemble, code)
			}

			_, ok := c.Get("default", "brown")
			So(ok, ShouldBeFalse)
		})

		Convey("... Adding a new item, and then retrieving it works as expected.", func() {
			black := []byte("#FFFFFF")
			err := c.Set("default", "black", black)
			So(err, ShouldBeNil)

			color, ok := c.Get("default", "black")
			So(ok, ShouldBeTrue)
			So(color, ShouldResemble, black)

			Convey("... Removing that, and then retrieving it works as expected.", func() {
				c.Remove("default", "black")

				_, ok = c.Get("default", "black")
				So(ok, ShouldBeFalse)
			})
		})

		Convey("... Adding a new item with an explicit expiration, and then retrieving it after expiration works as expected.", func() {
			black := []byte("#FFFFFF")
			err := c.SetToExpireAt("default", "black", time.Now().Add(5*time.Millisecond), black)
			So(err, ShouldBeNil)

			time.Sleep(10 * time.Millisecond)

			color, ok := c.Get("default", "black")
			So(ok, ShouldBeFalse)
			So(color, ShouldEqual, ItemNotFoundError)

			Convey("... Removing that, and then retrieving it works as expected.", func() {
				c.Remove("default", "black")

				_, ok = c.Get("default", "black")
				So(ok, ShouldBeFalse)
			})
		})

		Convey("... Asking for prefixed items, hits the prefix-specific BackFillFuncs", func() {
			// Get passwd
			pline, ok := c.Get("passwd", "black")
			So(ok, ShouldBeTrue)
			So(pline, ShouldResemble, []byte("#FFFFFF"))

			// Get group
			gline, ok := c.Get("group", "white")
			So(ok, ShouldBeTrue)
			So(gline, ShouldResemble, []byte("#000000"))

			// Get a non-registered prefix
			err, ok := c.Get("bogus", "white")
			So(ok, ShouldBeFalse)
			So(err, ShouldEqual, CacheNotFoundError)

			Convey("... Overriding prefixed items works as expected, as does removing the override", func() {
				// Override a group item
				err := c.Set("group", "white", []byte("#FFFFFF"))
				So(err, ShouldBeNil)
				sline, ok := c.Get("group", "white")
				So(ok, ShouldBeTrue)
				So(sline, ShouldResemble, []byte("#FFFFFF"))

				// Wait for the overridden group item to expire
				time.Sleep(5 * time.Millisecond)
				sline, ok = c.Get("group", "white")
				So(ok, ShouldBeTrue)
				So(sline, ShouldResemble, []byte("#000000")) // back to normal

			})

			Convey("... Setting and removing items from non-existent caches is as-expected", func() {

				err := c.Set("nope", "white", []byte("#FFFFFF"))
				So(err, ShouldEqual, CacheNotFoundError)

				err = c.Remove("nope", "white")
				So(err, ShouldEqual, CacheNotFoundError)
			})
		})

		Convey("... Checking stats works as-expected", func() {
			stb, err := c.stats()
			So(err, ShouldBeNil)
			x := make([]interface{}, 0)
			jerr := json.Unmarshal(stb, &x)
			So(jerr, ShouldBeNil)
			So(x, ShouldNotBeEmpty)

		})
	})
}

func concatPrefixKey(prefix, sep, key string) string {
	return prefix + sep + key
}

type ctxKey string

const prefixKey = ctxKey("prefix")

func Benchmark_Sprintf(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("%s%s%s", "stuff", "\t", "words")
	}
}

func Benchmark_Concat(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = "stuff" + "\t" + "words"
	}
}

func Benchmark_ConcatFunc(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = concatPrefixKey("stuff", "\t", "words")
	}
}

func Benchmark_SplitNo(b *testing.B) {
	key := "123"
	sep := "\t"
	for i := 0; i < b.N; i++ {
		if p := strings.SplitN(key, sep, 2); len(p) > 1 {
			_ = p
		}
	}
}

func Benchmark_SplitYes(b *testing.B) {
	key := "456\t123"
	sep := "\t"
	for i := 0; i < b.N; i++ {
		if p := strings.SplitN(key, sep, 2); len(p) > 1 {
			_ = p
		}
	}
}

func Benchmark_SplitHasNo(b *testing.B) {
	key := "123"
	sep := "\t"
	for i := 0; i < b.N; i++ {
		if strings.Contains(key, sep) {
			_ = strings.SplitN(key, sep, 2)
		}
	}
}

func Benchmark_SplitHasYes(b *testing.B) {
	key := "456\t123"
	sep := "\t"
	for i := 0; i < b.N; i++ {
		if strings.Contains(key, sep) {
			_ = strings.SplitN(key, sep, 2)
		}
	}
}

func Benchmark_SplitContextNo(b *testing.B) {
	key := "123"
	sep := "\t"
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		if x := ctx.Value(prefixKey); x != nil {
			prefix := x.(string)
			key = strings.TrimPrefix(key, prefix+sep)
		}
	}
}

func Benchmark_SplitContextYes(b *testing.B) {
	key := "456\t123"
	sep := "\t"
	ctx := context.WithValue(context.Background(), prefixKey, "456")
	for i := 0; i < b.N; i++ {
		if x := ctx.Value("prefix"); x != nil {
			prefix := x.(string)
			key = strings.TrimPrefix(key, prefix+sep)
		}
	}
}
