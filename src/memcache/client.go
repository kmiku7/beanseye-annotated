/*
 * memcache client
 */

package memcache

import (
	"errors"
	"math"
	"sync"
	"time"
)

const (
	CMD_SET    = 0
	CMD_DELETE = 1
	CMD_INCR   = 2
	CMD_APPEND = 3
)

// Client of memcached
type Client struct {
	scheduler Scheduler
	N, W, R   int
	success   chan bool
}

func NewClient(sch Scheduler, N, W, R int) (c *Client) {
	c = new(Client)
	c.scheduler = sch
	c.N = N
	c.W = W
	c.R = R
	return c
}

func (c *Client) Get(key string) (r *Item, targets []string, err error) {
	hosts := c.scheduler.GetHostsByKey(key)
	cnt := 0
	for i, host := range hosts {
		st := time.Now()
		r, err = host.Get(key)
		if err != nil {
			c.scheduler.Feedback(host, key, -10, true)
		} else {
			cnt++
			if r != nil {
				t := float64(time.Now().Sub(st)) / 1e9
				c.scheduler.Feedback(host, key, -float64(math.Sqrt(t)*t), false)
				for j := 0; j < i; j++ {
					c.scheduler.Feedback(hosts[j], key, -1, false)
				}
				// got the right rval
				targets = []string{host.Addr}
				err = nil
				//return r, nil
				return
			}
		}
		if cnt >= c.R && i+1 >= c.N {
			// because hosts are sorted
			err = nil
			for _, fail_host := range hosts[:3] {
				targets = append(targets, fail_host.Addr)
			}
			// because no item gotten
			break
		}
	}
	// here is a failure exit
	return
}

func (c *Client) getMulti(keys []string) (rs map[string]*Item, targets []string, err error) {
	need := len(keys)
	rs = make(map[string]*Item, need)
	hosts := c.scheduler.GetHostsByKey(keys[0])
	suc := 0
	for i, host := range hosts {
		st := time.Now()
		r, er := host.GetMulti(keys)
		if er != nil { // failed
			err = er
			c.scheduler.Feedback(host, keys[0], -10, true)
		} else {
			suc += 1
			targets = append(targets, host.Addr)
		}

		t := float64(time.Now().Sub(st)) / 1e9
		c.scheduler.Feedback(host, keys[0], -float64(math.Sqrt(t)*t), false)
		for k, v := range r {
			rs[k] = v
		}

		if len(rs) == need {
			break
		}
		if i+1 >= c.N && suc >= c.R {
			err = nil
			targets = []string{}
			break
		}

		new_keys := []string{}
		for _, k := range keys {
			if _, ok := rs[k]; !ok {
				new_keys = append(new_keys, k)
			}
		}
		keys = new_keys
		if len(keys) == 0 {
			break // repeated keys
		}
	}
	if len(rs) > 0 {
		err = nil
	}
	return
}

func (c *Client) GetMulti(keys []string) (rs map[string]*Item, targets []string, err error) {
	var lock sync.Mutex
	rs = make(map[string]*Item, len(keys))

	gs := c.scheduler.DivideKeysByBucket(keys)
	reply := make(chan bool, len(gs))
	for _, ks := range gs {
		if len(ks) > 0 {
			go func(keys []string) {
				r, t, e := c.getMulti(keys)
				if e != nil {
					err = e
				} else {
					for k, v := range r {
						lock.Lock()
						rs[k] = v
						targets = append(targets, t...)
						lock.Unlock()
					}
				}
				reply <- true
			}(ks)
		} else {
			reply <- true
		}
	}
	// wait for complete
	for _, _ = range gs {
		<-reply
	}
	return
}

func (c *Client) Set(key string, item *Item, noreply bool) (ok bool, targets []string, final_err error) {
	suc := 0
	for i, host := range c.scheduler.GetHostsByKey(key) {
		if ok, err := host.Set(key, item, noreply); err == nil && ok {
			suc++
			targets = append(targets, host.Addr)
		} else {
			c.scheduler.Feedback(host, key, -2, false)
		}
		if suc >= c.W && (i+1) >= c.N {
			// at least try N backends, and succeed W backends
			break
		}
	}
	if suc < c.W {
		ok = false
		final_err = errors.New("write failed")
		return
	}
	ok = true
	return
}

func (c *Client) Append(key string, value []byte) (ok bool, targets []string, final_err error) {
	suc := 0
	for i, host := range c.scheduler.GetHostsByKey(key) {
		if ok, err := host.Append(key, value); err == nil && ok {
			suc++
			targets = append(targets, host.Addr)
		}
		if suc >= c.W && (i+1) >= c.N {
			// at least try N backends, and succeed W backends
			break
		}
	}
	if suc < c.W {
		ok = false
		final_err = errors.New("write failed")
		return
	}
    ok = true
	return
}

func (c *Client) Incr(key string, value int) (result int, targets []string, err error) {
	//result := 0
	suc := 0
	for i, host := range c.scheduler.GetHostsByKey(key) {
		r, e := host.Incr(key, value)
		if e != nil {
			err = e
			continue
		}
		if r > 0 {
			suc++
			targets = append(targets, host.Addr)
		}
		if r > result {
			result = r
		}
		if suc >= c.W && (i+1) >= c.N {
			// at least try N backends, and succeed W backends
			break
		}
	}
	if result > 0 {
		err = nil
	}
	//return result, err // maximize
	return
}

func (c *Client) Delete(key string) (r bool, targets []string, err error) {
	suc := 0
	err_count := 0
	for _, host := range c.scheduler.GetHostsByKey(key) {
		ok, er := host.Delete(key)
		if er != nil { // means in err occurs in the right bucket
			err = er
			err_count++
		} else if ok {
			suc++
			targets = append(targets, host.Addr)
		}
		if suc >= c.N {
            break
		}
	}
	if suc > 0 || err_count < 2 {
		// if success at least one, or not failed twice
        err = nil
        r = true
	} else {
	    r = false
    }
	//return suc >= c.W, err
	return
}

func (c *Client) Len() int {
	return 0
}
