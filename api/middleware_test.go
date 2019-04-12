package api

import (
	"net/http"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestGetAuthCreds(t *testing.T) {
	Convey("When authenticating with no auth headers", t, func(c C) {
		req, _ := http.NewRequest("GET", "/", nil)
		user, pass := getAuthCreds(req)
		c.So(user, ShouldEqual, "")
		c.So(pass, ShouldEqual, "")
	})
	Convey("When authenticating with basicAuth", t, func(c C) {
		req, _ := http.NewRequest("GET", "/", nil)
		req.SetBasicAuth("foo", "bar")
		user, pass := getAuthCreds(req)
		c.So(user, ShouldEqual, "foo")
		c.So(pass, ShouldEqual, "bar")
	})
	Convey("When authenticating with bearer without instance_id", t, func(c C) {
		req, _ := http.NewRequest("GET", "/", nil)
		req.Header.Add("Authorization", "Bearer abcdefg")
		user, pass := getAuthCreds(req)
		c.So(user, ShouldEqual, "api_key")
		c.So(pass, ShouldEqual, "abcdefg")
	})
	Convey("When authenticating with bearer with instance_id", t, func(c C) {
		req, _ := http.NewRequest("GET", "/", nil)
		req.Header.Add("Authorization", "Bearer 4:abcdefg")
		user, pass := getAuthCreds(req)
		c.So(user, ShouldEqual, "4")
		c.So(pass, ShouldEqual, "abcdefg")
	})
	Convey("When authenticating with basicAuth and bearer", t, func(c C) {
		req, _ := http.NewRequest("GET", "/", nil)
		req.Header.Add("Authorization", "Bearer 4:abcdefg")
		req.SetBasicAuth("foo", "bar")
		user, pass := getAuthCreds(req)
		c.So(user, ShouldEqual, "foo")
		c.So(pass, ShouldEqual, "bar")
	})
}
