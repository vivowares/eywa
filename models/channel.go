package models

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"github.com/vivowares/eywa/Godeps/_workspace/src/github.com/speps/go-hashids"
	"github.com/vivowares/eywa/Godeps/_workspace/src/gopkg.in/olivere/elastic.v3"
	"github.com/vivowares/eywa/connections"
	. "github.com/vivowares/eywa/utils"

)

var SupportedDataTypes = []string{"float", "int", "boolean", "string"}
var InternalTags = []string{"_ip"}
var Salt = "Cc4D5xBlbCBqYTuimuNPGsio7YoMo8d8"
var HashLen = 16

type Channel struct {
	Id              int         `sql:"type:integer primary key autoincrement" json:"-"`
	Name            string      `sql:"type:varchar(255);unique_index" json:"name"`
	Description     string      `sql:"type:text" json:"description"`
	Tags            StringSlice `sql:"type:text" json:"tags"`
	Fields          StringMap   `sql:"type:text" json:"fields"`
	MessageHandlers StringSlice `sql:"type:text" json:"-"`
	AccessTokens    StringSlice `sql:"type:text" json:"access_tokens"`
}

func (c *Channel) validate() error {
	if len(c.Name) == 0 {
		return errors.New("name is empty")
	}

	if len(c.Description) == 0 {
		return errors.New("description is empty")
	}

	if c.Tags == nil {
		c.Tags = StringSlice(make([]string, 0))
	}

	if c.Fields == nil {
		c.Fields = StringMap(make(map[string]string, 0))
	}

	// enable indexer on all channels
	// if c.MessageHandlers == nil {
	// 	c.MessageHandlers = StringSlice(make([]string, 0))
	// }
	c.MessageHandlers = []string{"indexer"}

	if c.AccessTokens == nil {
		c.AccessTokens = StringSlice(make([]string, 0))
	}

	// skip this validation for now
	// for _, h := range c.MessageHandlers {
	// 	if _, found := SupportedMessageHandlers[h]; !found {
	// 		return errors.New("unsupported message handler: " + h)
	// 	}
	// }

	if len(c.AccessTokens) == 0 {
		return errors.New("access_tokens are empty")
	}

	if len(c.Tags) > 64 {
		return errors.New("too many tags, at most 64 tags are supported")
	}

	tagMap := make(map[string]bool, 0)

	for _, tagName := range c.Tags {
		if !AlphaNumeric(tagName) {
			return errors.New("invalid tag name, only letters, numbers and underscores are allowed")
		}

		if _, found := tagMap[tagName]; found {
			return errors.New(fmt.Sprintf("duplicate tag name: %s", tagName))
		} else {
			tagMap[tagName] = true

			if _, found = c.Fields[tagName]; found {
				return errors.New(fmt.Sprintf("conflicting tag name: %s defined in fields", tagName))
			}
		}
	}

	if len(c.Fields) == 0 {
		return errors.New("fields are empty")
	}

	if len(c.Fields) > 128 {
		return errors.New("too many fields, at most 128 fields are supported")
	}

	for k, v := range c.Fields {
		if !AlphaNumeric(k) {
			return errors.New("invalid field name, only letters, numbers and underscores are allowed")
		}

		if !StringSliceContains(SupportedDataTypes, v) {
			return errors.New(fmt.Sprintf("unsupported datatype on %s: %s, supported datatypes are %s", k, v, strings.Join(SupportedDataTypes, ",")))
		}
	}

	return nil
}

func (c *Channel) BeforeCreate() error {
	return c.validate()
}

func (c *Channel) AfterCreate() error {
	name, err := c.HashId()
	if err != nil {
		return err
	}
	_, err = connections.NewConnectionManager(name)
	return err
}

func (c *Channel) AfterDelete() error {
	name, err := c.HashId()
	if err != nil {
		return err
	}
	return connections.CloseConnectionManager(name)
}

func (c *Channel) BeforeUpdate() error {
	ch := &Channel{}
	if found := ch.FindById(c.Id); !found {
		return errors.New("record not found")
	}

	//removing a tag is not allowed
	for _, t := range ch.Tags {
		if !StringSliceContains(c.Tags, t) {
			return errors.New("removing a tag is not allowed: " + t)
		}
	}

	// removing or modifying a field is not allowed
	for k, v := range ch.Fields {
		if fv, found := c.Fields[k]; !found {
			return errors.New("removing a field is not allowed: " + k)
		} else if v != fv {
			return errors.New("changing a field type is not allowed: " + k)
		}
	}

	return c.validate()
}

func (c *Channel) Create() error {
	return DB.Create(c).Error
}

func (c *Channel) Delete() error {
	return DB.Delete(c).Error
}

func (c *Channel) Update() error {
	return DB.Save(c).Error
}

func (c *Channel) FindById(id int) bool {
	DB.First(c, id)
	return !DB.NewRecord(c)
}

func Channels() []*Channel {
	chs := []*Channel{}
	DB.Find(&chs)
	return chs
}

func (c *Channel) HashId() (string, error) {
	hd := hashids.NewData()
	hd.Salt = Salt
	hd.MinLength = HashLen
	h := hashids.NewWithData(hd)
	return h.Encode([]int{c.Id})
}

func (c *Channel) IndexStats() (*elastic.IndicesStatsResponse, error) {
	return IndexClient.IndexStats().Index(GlobalIndexName(c)).Do()
}

func (c *Channel) Indices() []string {
	indices := []string{}
	stats, found := FetchCachedChannelIndexStatsById(c.Id)
	if found && stats.Indices != nil {
		for k, _ := range stats.Indices {
			indices = append(indices, k)
		}
	}
	return indices
}

func (c *Channel) DeleteIndices() error {
	_, err := IndexClient.DeleteIndex().Index([]string{GlobalIndexName(c)}).Do()
	return err
}

func FetchCachedChannelById(id int) (*Channel, bool) {
	cacheKey := fmt.Sprintf("cache.channel:%d", id)
	ch, err := Cache.Fetch(cacheKey, 1*time.Minute, func() (interface{}, error) {
		c := &Channel{}
		found := c.FindById(id)
		if found {
			return c, nil
		} else {
			return nil, errors.New("channel not found")
		}
	})

	if err == nil {
		return ch.(*Channel), true
	} else {
		return nil, false
	}
}

func FetchCachedChannelIndexStatsById(id int) (*elastic.IndicesStatsResponse, bool) {
	cacheKey := fmt.Sprintf("cache.channel_stats:%d", id)
	resp, err := Cache.Fetch(cacheKey, 1*time.Minute, func() (interface{}, error) {
		c := &Channel{}
		found := c.FindById(id)
		if !found {
			return nil, errors.New("channel not found")
		} else {
			return c.IndexStats()
		}
	})

	if err == nil {
		return resp.(*elastic.IndicesStatsResponse), true
	} else {
		return nil, false
	}
}

func DecodeHashId(hash string) int {
	hd := hashids.NewData()
	hd.Salt = Salt
	hd.MinLength = HashLen
	h := hashids.NewWithData(hd)
	return h.Decode(hash)[0]
}
