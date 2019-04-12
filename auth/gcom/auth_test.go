package gcom

import (
	"flag"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jarcoal/httpmock"
	. "github.com/smartystreets/goconvey/convey"
)

func TestFlags(t *testing.T) {
	Convey("When setting auth-valid-org-id to empty string", t, func(c C) {
		validOrgIds = int64SliceFlag{}
		err := flag.Set("auth-valid-org-id", "")
		c.So(err, ShouldBeNil)
		c.So(validOrgIds, ShouldHaveLength, 0)
	})
	Convey("When setting auth-valid-org-id has no values", t, func(c C) {
		validOrgIds = int64SliceFlag{}
		err := flag.Set("auth-valid-org-id", ", ")
		c.So(err, ShouldBeNil)
		c.So(validOrgIds, ShouldHaveLength, 0)
	})

	Convey("When setting auth-valid-org-id to invalid value", t, func(c C) {
		validOrgIds = int64SliceFlag{}
		err := flag.Set("auth-valid-org-id", "foo")
		c.So(err, ShouldHaveSameTypeAs, &strconv.NumError{})
	})

	Convey("When setting auth-valid-org-id to single org", t, func(c C) {
		validOrgIds = int64SliceFlag{}
		err := flag.Set("auth-valid-org-id", "10")
		c.So(err, ShouldBeNil)
		c.So(validOrgIds, ShouldHaveLength, 1)
		c.So(validOrgIds[0], ShouldEqual, 10)
	})

	Convey("When setting auth-valid-org-id to many orgs", t, func(c C) {
		validOrgIds = int64SliceFlag{}
		err := flag.Set("auth-valid-org-id", "10,1,17")
		c.So(err, ShouldBeNil)
		c.So(validOrgIds, ShouldHaveLength, 3)
		c.So(validOrgIds[0], ShouldEqual, 10)
		c.So(validOrgIds[1], ShouldEqual, 1)
		c.So(validOrgIds[2], ShouldEqual, 17)
	})

	Convey("When auth-valid-org-id setting has spaces", t, func(c C) {
		validOrgIds = int64SliceFlag{}
		err := flag.Set("auth-valid-org-id", " 10 , 1, 17")
		c.So(err, ShouldBeNil)
		c.So(validOrgIds, ShouldHaveLength, 3)
		c.So(validOrgIds[0], ShouldEqual, 10)
		c.So(validOrgIds[1], ShouldEqual, 1)
		c.So(validOrgIds[2], ShouldEqual, 17)
	})

	Convey("When auth-valid-org-id setting has repeated commas", t, func(c C) {
		validOrgIds = int64SliceFlag{}
		err := flag.Set("auth-valid-org-id", ",,,10")
		c.So(err, ShouldBeNil)
		c.So(validOrgIds, ShouldHaveLength, 1)
	})
}

func TestAuth(t *testing.T) {
	mockTransport := httpmock.NewMockTransport()
	client.Transport = mockTransport
	validOrgIds = int64SliceFlag{}
	testUser := SignedInUser{
		Id:        3,
		OrgName:   "awoods Test",
		OrgSlug:   "awoodsTest",
		OrgId:     2,
		Name:      "testKey",
		Role:      ROLE_EDITOR,
		CreatedAt: time.Now(),
		key:       "foo",
	}

	tokenCache = &TokenCache{items: make(map[string]*TokenResp), cacheTTL: time.Millisecond * 10}

	Convey("When authenticating with adminKey", t, func(c C) {
		user, err := Auth("key", "key")
		c.So(err, ShouldBeNil)
		c.So(user.Role, ShouldEqual, ROLE_ADMIN)
		c.So(user.OrgId, ShouldEqual, 1)
		c.So(user.OrgName, ShouldEqual, "Admin")
		c.So(user.IsAdmin, ShouldEqual, true)
		c.So(user.key, ShouldEqual, "key")
	})
	Convey("when authenticating with valid Key", t, func(c C) {
		responder, err := httpmock.NewJsonResponder(200, &testUser)
		c.So(err, ShouldBeNil)
		mockTransport.RegisterResponder("POST", "https://grafana.com/api/api-keys/check", responder)

		user, err := Auth("key", "foo")
		c.So(err, ShouldBeNil)
		c.So(user.Role, ShouldEqual, testUser.Role)
		c.So(user.OrgId, ShouldEqual, testUser.OrgId)
		c.So(user.OrgName, ShouldEqual, testUser.OrgName)
		c.So(user.OrgSlug, ShouldEqual, testUser.OrgSlug)
		c.So(user.IsAdmin, ShouldEqual, testUser.IsAdmin)
		c.So(user.key, ShouldEqual, testUser.key)
		mockTransport.Reset()
	})

	Convey("When authenticating using cache", t, func(c C) {
		tokenCache.Set("foo", &testUser)
		mockTransport.RegisterNoResponder(func(req *http.Request) (*http.Response, error) {
			t.Fatalf("unexpected request made. %s %s", req.Method, req.URL.String())
			return nil, nil
		})
		user, err := Auth("key", "foo")
		c.So(err, ShouldBeNil)
		c.So(user.Role, ShouldEqual, testUser.Role)
		c.So(user.OrgId, ShouldEqual, testUser.OrgId)
		c.So(user.OrgName, ShouldEqual, testUser.OrgName)
		c.So(user.OrgSlug, ShouldEqual, testUser.OrgSlug)
		c.So(user.IsAdmin, ShouldEqual, testUser.IsAdmin)
		c.So(user.key, ShouldEqual, testUser.key)
		mockTransport.Reset()
	})

	Convey("When authenticating with invalid org id 1", t, func(c C) {
		tokenCache.Clear()
		responder, err := httpmock.NewJsonResponder(200, &testUser)
		c.So(err, ShouldBeNil)
		mockTransport.RegisterResponder("POST", "https://grafana.com/api/api-keys/check", responder)

		originalValidOrgIds := validOrgIds
		defer func() { validOrgIds = originalValidOrgIds }()
		validOrgIds = int64SliceFlag{1}

		user, err := Auth("key", "foo")
		c.So(user, ShouldBeNil)
		c.So(err, ShouldEqual, ErrInvalidOrgId)
		mockTransport.Reset()
	})

	Convey("When authenticating with invalid org id 2", t, func(c C) {
		tokenCache.Clear()
		responder, err := httpmock.NewJsonResponder(200, &testUser)
		c.So(err, ShouldBeNil)
		mockTransport.RegisterResponder("POST", "https://grafana.com/api/api-keys/check", responder)

		originalValidOrgIds := validOrgIds
		defer func() { validOrgIds = originalValidOrgIds }()

		validOrgIds = int64SliceFlag{3, 4, 5}
		user, err := Auth("key", "foo")
		c.So(user, ShouldBeNil)
		c.So(err, ShouldEqual, ErrInvalidOrgId)
		mockTransport.Reset()
	})

	Convey("When authenticating with explicitely valid org id", t, func(c C) {
		tokenCache.Clear()
		responder, err := httpmock.NewJsonResponder(200, &testUser)
		c.So(err, ShouldBeNil)
		mockTransport.RegisterResponder("POST", "https://grafana.com/api/api-keys/check", responder)

		originalValidOrgIds := validOrgIds
		defer func() { validOrgIds = originalValidOrgIds }()

		validOrgIds = int64SliceFlag{1, 2, 3, 4}
		user, err := Auth("key", "foo")
		c.So(err, ShouldBeNil)
		c.So(user.Role, ShouldEqual, testUser.Role)
		c.So(user.OrgId, ShouldEqual, testUser.OrgId)
		c.So(user.OrgName, ShouldEqual, testUser.OrgName)
		c.So(user.OrgSlug, ShouldEqual, testUser.OrgSlug)
		c.So(user.IsAdmin, ShouldEqual, testUser.IsAdmin)
		c.So(user.key, ShouldEqual, testUser.key)
		mockTransport.Reset()
	})

	Convey("When cached entry is expired", t, func(c C) {
		tc := &TokenCache{
			items:    make(map[string]*TokenResp),
			stop:     make(chan struct{}),
			cacheTTL: time.Millisecond * 10,
		}
		tc.Set("bar", &testUser)
		newUser := SignedInUser{
			Id:        3,
			OrgName:   "foo",
			OrgSlug:   "foo",
			OrgId:     2,
			Name:      "foo",
			Role:      ROLE_EDITOR,
			CreatedAt: time.Now(),
			key:       "bar",
		}
		mockTransport.RegisterResponder("POST", "https://grafana.com/api/api-keys/check",
			func(req *http.Request) (*http.Response, error) {
				resp, err := httpmock.NewJsonResponse(200, &newUser)
				if err != nil {
					return httpmock.NewStringResponse(500, ""), nil
				}
				go func() {
					time.Sleep(time.Millisecond)
					close(tc.stop)
				}()
				return resp, nil
			},
		)

		// make sure key is cached
		cuser, valid := tc.Get("bar")
		c.So(cuser, ShouldNotBeNil)
		c.So(valid, ShouldBeTrue)

		// start background task to reValidate our token
		go tc.backgroundValidation()
		select {
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for token to be revalidated")
		case <-tc.stop:
		}

		// make sure cache is now updated.
		user, valid := tc.Get("bar")
		c.So(user, ShouldNotBeNil)
		c.So(valid, ShouldBeTrue)
		c.So(user.Role, ShouldEqual, newUser.Role)
		c.So(user.OrgId, ShouldEqual, newUser.OrgId)
		c.So(user.OrgName, ShouldEqual, newUser.OrgName)
		c.So(user.OrgSlug, ShouldEqual, newUser.OrgSlug)
		c.So(user.IsAdmin, ShouldEqual, newUser.IsAdmin)
		c.So(user.key, ShouldEqual, newUser.key)
		mockTransport.Reset()
	})

	Convey("When token has not been seen for more than cachettl", t, func(c C) {
		tc := &TokenCache{
			items:    make(map[string]*TokenResp),
			stop:     make(chan struct{}),
			cacheTTL: time.Millisecond * 10,
		}
		tc.Set("bar", &testUser)
		tokenCache.stop = make(chan struct{})
		mockTransport.RegisterResponder("POST", "https://grafana.com/api/api-keys/check",
			func(req *http.Request) (*http.Response, error) {
				resp, err := httpmock.NewJsonResponse(200, &testUser)
				if err != nil {
					return httpmock.NewStringResponse(500, ""), nil
				}
				go func() {
					time.Sleep(time.Millisecond)
					close(tc.stop)
				}()
				return resp, nil
			},
		)

		// make sure key is cached
		cuser, valid := tc.Get("bar")
		c.So(cuser, ShouldNotBeNil)
		c.So(valid, ShouldBeTrue)

		tc.items["bar"].lastRead = 0

		// start background task to purge our token
		go tc.backgroundValidation()
		select {
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for token to be revalidated")
		case <-tc.stop:
		}

		user, valid := tc.Get("bar")
		c.So(user, ShouldBeNil)
		c.So(valid, ShouldBeFalse)
		mockTransport.Reset()
	})

	Convey("When concurrent requests for uncached token", t, func(c C) {
		mu := sync.Mutex{}
		reqCount := 0
		mockTransport.RegisterResponder("POST", "https://grafana.com/api/api-keys/check",
			func(req *http.Request) (*http.Response, error) {
				mu.Lock()
				reqCount++
				mu.Unlock()
				time.Sleep(time.Millisecond * 50)
				resp, err := httpmock.NewJsonResponse(200, &testUser)
				if err != nil {
					return httpmock.NewStringResponse(500, ""), nil
				}
				return resp, nil
			},
		)
		type resp struct {
			user *SignedInUser
			err  error
		}
		ch := make(chan resp)
		wg := sync.WaitGroup{}
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				user, err := ValidateToken("foo")
				ch <- resp{user: user, err: err}
				wg.Done()
			}()
		}

		go func() {
			wg.Wait()
			close(ch)
		}()

		for r := range ch {
			c.So(r.err, ShouldBeNil)
			c.So(r.user.Role, ShouldEqual, testUser.Role)
			c.So(r.user.OrgId, ShouldEqual, testUser.OrgId)
			c.So(r.user.OrgName, ShouldEqual, testUser.OrgName)
			c.So(r.user.OrgSlug, ShouldEqual, testUser.OrgSlug)
			c.So(r.user.IsAdmin, ShouldEqual, testUser.IsAdmin)
			c.So(r.user.key, ShouldEqual, testUser.key)
		}
		c.So(reqCount, ShouldEqual, 1)
	})
}

func TestCheckInstance(t *testing.T) {
	mockTransport := httpmock.NewMockTransport()
	client.Transport = mockTransport
	testUser := SignedInUser{
		Id:        3,
		OrgName:   "awoods Test",
		OrgSlug:   "awoodsTest",
		OrgId:     2,
		Name:      "testKey",
		Role:      ROLE_EDITOR,
		CreatedAt: time.Now(),
		key:       "foo",
	}

	testInstance := Instance{
		ID:           10,
		OrgID:        3,
		InstanceType: "graphite",
	}

	tokenCache = &TokenCache{
		items:    make(map[string]*TokenResp),
		cacheTTL: time.Millisecond * 10,
	}
	instanceCache = &InstanceCache{
		items:    make(map[string]*InstanceResp),
		cacheTTL: time.Millisecond * 10,
	}

	Convey("when checking valid instanceID", t, func(c C) {
		responder, err := httpmock.NewJsonResponder(200, &testInstance)
		c.So(err, ShouldBeNil)
		mockTransport.RegisterResponder("GET", "https://grafana.com/api/hosted-metrics/10", responder)
		instanceCache.Clear()
		// instance should not be cached.
		valid, cached := instanceCache.Get(fmt.Sprintf("%s:%s", "10", testUser.key))
		c.So(valid, ShouldBeFalse)
		c.So(cached, ShouldBeFalse)

		err = testUser.CheckInstance("10")
		c.So(err, ShouldBeNil)
		mockTransport.Reset()

		valid, cached = instanceCache.Get(fmt.Sprintf("%s:%s", "10", testUser.key))
		c.So(valid, ShouldBeTrue)
		c.So(cached, ShouldBeTrue)
	})

	Convey("when checking cached valid instanceID", t, func(c C) {
		responder, err := httpmock.NewJsonResponder(404, "not found")
		c.So(err, ShouldBeNil)
		mockTransport.RegisterResponder("GET", "https://grafana.com/api/hosted-metrics/10", responder)

		instanceCache.Set(fmt.Sprintf("%s:%s", "10", testUser.key), true)
		err = testUser.CheckInstance("10")
		c.So(err, ShouldEqual, nil)
		mockTransport.Reset()
	})
	Convey("when checking instanceID and g.com is down", t, func(c C) {
		mockTransport.RegisterResponder("GET", "https://grafana.com/api/hosted-metrics/10", func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("failed")
		})
		instanceCache.Clear()
		err := testUser.CheckInstance("10")
		c.So(err.Error(), ShouldEqual, "Get https://grafana.com/api/hosted-metrics/10: failed")
		mockTransport.Reset()
	})
	Convey("when checking invalid instanceID", t, func(c C) {
		responder, err := httpmock.NewJsonResponder(404, "not found")
		c.So(err, ShouldBeNil)
		mockTransport.RegisterResponder("GET", "https://grafana.com/api/hosted-metrics/20", responder)

		err = testUser.CheckInstance("20")
		c.So(err, ShouldEqual, ErrInvalidInstanceID)
		mockTransport.Reset()
	})
	Convey("when checking cached invalid instanceID", t, func(c C) {
		responder, err := httpmock.NewJsonResponder(500, "err")
		c.So(err, ShouldBeNil)
		mockTransport.RegisterResponder("GET", "https://grafana.com/api/hosted-metrics/20", responder)
		instanceCache.Set(fmt.Sprintf("%s:%s", "20", testUser.key), false)
		err = testUser.CheckInstance("20")
		c.So(err, ShouldEqual, ErrInvalidInstanceID)
		mockTransport.Reset()
	})

	Convey("When cached entry is expired", t, func(c C) {
		ic := &InstanceCache{
			items: make(map[string]*InstanceResp),
			stop:  make(chan struct{}),

			cacheTTL: time.Millisecond * 10,
		}
		ic.Set(fmt.Sprintf("%s:%s", "10", testUser.key), true)

		mockTransport.RegisterResponder("GET", "https://grafana.com/api/hosted-metrics/10",
			func(req *http.Request) (*http.Response, error) {
				go func() {
					time.Sleep(time.Millisecond)
					close(ic.stop)
				}()
				return httpmock.NewStringResponse(404, "not found"), nil
			},
		)

		// make sure key is cached
		valid, cached := ic.Get(fmt.Sprintf("%s:%s", "10", testUser.key))
		c.So(valid, ShouldBeTrue)
		c.So(cached, ShouldBeTrue)

		// start background task to reValidate our token
		go ic.backgroundValidation()
		select {
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for token to be revalidated")
		case <-ic.stop:
		}

		// make sure cache is now updated.
		valid, cached = ic.Get(fmt.Sprintf("%s:%s", "10", testUser.key))
		c.So(valid, ShouldBeFalse)
		c.So(cached, ShouldBeTrue)
		mockTransport.Reset()
	})

	Convey("When instance has not been seen for more than cachettl", t, func(c C) {
		ic := &InstanceCache{
			items:    make(map[string]*InstanceResp),
			stop:     make(chan struct{}),
			cacheTTL: time.Millisecond * 10,
		}
		ic.Set(fmt.Sprintf("%s:%s", "10", testUser.key), true)

		mockTransport.RegisterResponder("GET", "https://grafana.com/api/hosted-metrics/10",
			func(req *http.Request) (*http.Response, error) {
				go func() {
					time.Sleep(time.Millisecond)
					close(ic.stop)
				}()
				return httpmock.NewStringResponse(404, "not found"), nil
			},
		)

		// make sure key is cached
		valid, cached := ic.Get(fmt.Sprintf("%s:%s", "10", testUser.key))
		c.So(valid, ShouldBeTrue)
		c.So(cached, ShouldBeTrue)

		ic.items[fmt.Sprintf("%s:%s", "10", testUser.key)].lastRead = 0

		// start background task to purge our token
		go ic.backgroundValidation()
		select {
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for token to be revalidated")
		case <-ic.stop:
		}

		valid, cached = ic.Get(fmt.Sprintf("%s:%s", "10", testUser.key))
		c.So(valid, ShouldBeFalse)
		c.So(cached, ShouldBeFalse)

		mockTransport.Reset()
	})

	Convey("When concurrent requests for uncached instance", t, func(c C) {
		mu := sync.Mutex{}
		reqCount := 0
		mockTransport.RegisterResponder("GET", "https://grafana.com/api/hosted-metrics/10",
			func(req *http.Request) (*http.Response, error) {
				mu.Lock()
				reqCount++
				mu.Unlock()
				time.Sleep(time.Millisecond * 50)
				resp, err := httpmock.NewJsonResponse(200, &testInstance)
				if err != nil {
					return httpmock.NewStringResponse(500, ""), nil
				}
				return resp, nil
			},
		)
		ch := make(chan error)
		wg := sync.WaitGroup{}
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				err := ValidateInstance("10:foo")
				ch <- err
				wg.Done()
			}()
		}

		go func() {
			wg.Wait()
			close(ch)
		}()

		for err := range ch {
			c.So(err, ShouldBeNil)
		}
		c.So(reqCount, ShouldEqual, 1)
	})

	validInstanceType = "graphite"
	Convey("when checking valid instanceType", t, func(c C) {
		responder, err := httpmock.NewJsonResponder(200, &testInstance)
		c.So(err, ShouldBeNil)
		mockTransport.RegisterResponder("GET", "https://grafana.com/api/hosted-metrics/10", responder)
		instanceCache.Clear()
		// instance should not be cached.
		valid, cached := instanceCache.Get(fmt.Sprintf("%s:%s", "10", testUser.key))
		c.So(valid, ShouldBeFalse)
		c.So(cached, ShouldBeFalse)

		err = testUser.CheckInstance("10")
		c.So(err, ShouldBeNil)
		mockTransport.Reset()

		valid, cached = instanceCache.Get(fmt.Sprintf("%s:%s", "10", testUser.key))
		c.So(valid, ShouldBeTrue)
		c.So(cached, ShouldBeTrue)
	})

	validInstanceType = "cortex"
	validationDryRun = false
	Convey("when checking invalid instanceType", t, func(c C) {
		responder, err := httpmock.NewJsonResponder(200, &testInstance)
		c.So(err, ShouldBeNil)
		mockTransport.RegisterResponder("GET", "https://grafana.com/api/hosted-metrics/10", responder)
		instanceCache.Clear()
		// instance should not be cached.
		valid, cached := instanceCache.Get(fmt.Sprintf("%s:%s", "10", testUser.key))
		c.So(valid, ShouldBeFalse)
		c.So(cached, ShouldBeFalse)

		err = testUser.CheckInstance("10")
		c.So(err, ShouldEqual, ErrInvalidInstanceType)
		mockTransport.Reset()
	})

	testInstance = Instance{
		ID:           10,
		OrgID:        3,
		InstanceType: "logs",
	}

	validInstanceType = "logs"
	Convey("when checking valid logs instance", t, func(c C) {
		responder, err := httpmock.NewJsonResponder(200, &testInstance)
		c.So(err, ShouldBeNil)
		mockTransport.RegisterResponder("GET", "https://grafana.com/api/hosted-logs/10", responder)
		instanceCache.Clear()
		// instance should not be cached.
		valid, cached := instanceCache.Get(fmt.Sprintf("%s:%s", "10", testUser.key))
		c.So(valid, ShouldBeFalse)
		c.So(cached, ShouldBeFalse)

		err = testUser.CheckInstance("10")
		c.So(err, ShouldBeNil)
		mockTransport.Reset()

		valid, cached = instanceCache.Get(fmt.Sprintf("%s:%s", "10", testUser.key))
		c.So(valid, ShouldBeTrue)
		c.So(cached, ShouldBeTrue)
	})

	testInstance = Instance{
		ID:           10,
		OrgID:        3,
		InstanceType: "cortex",
		ClusterID:    15,
	}
	validInstanceType = "cortex"
	validClusterID = 15

	Convey("when checking valid clusterName", t, func(c C) {
		responder, err := httpmock.NewJsonResponder(200, &testInstance)
		c.So(err, ShouldBeNil)
		mockTransport.RegisterResponder("GET", "https://grafana.com/api/hosted-metrics/10", responder)
		instanceCache.Clear()
		// instance should not be cached.
		valid, cached := instanceCache.Get(fmt.Sprintf("%s:%s", "10", testUser.key))
		c.So(valid, ShouldBeFalse)
		c.So(cached, ShouldBeFalse)

		err = testUser.CheckInstance("10")
		c.So(err, ShouldBeNil)
		mockTransport.Reset()

		valid, cached = instanceCache.Get(fmt.Sprintf("%s:%s", "10", testUser.key))
		c.So(valid, ShouldBeTrue)
		c.So(cached, ShouldBeTrue)
	})

	validClusterID = 20
	Convey("when checking invalid clusterName", t, func(c C) {
		responder, err := httpmock.NewJsonResponder(200, &testInstance)
		c.So(err, ShouldBeNil)
		mockTransport.RegisterResponder("GET", "https://grafana.com/api/hosted-metrics/10", responder)
		instanceCache.Clear()
		// instance should not be cached.
		valid, cached := instanceCache.Get(fmt.Sprintf("%s:%s", "10", testUser.key))
		c.So(valid, ShouldBeFalse)
		c.So(cached, ShouldBeFalse)

		err = testUser.CheckInstance("10")
		c.So(err, ShouldEqual, ErrInvalidCluster)
		mockTransport.Reset()
	})
}
