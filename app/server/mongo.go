package server

import (
	"strings"
	"time"

	"github.com/globalsign/mgo"
	"github.com/globalsign/mgo/bson"
	log "github.com/go-pkgz/lgr"
	"github.com/go-pkgz/mongo"
	"github.com/pkg/errors"

	"github.com/umputun/dkll/app/core"
)

// Mongo store provides all mongo-related ops
type Mongo struct {
	*mongo.Connection
}

// NewMongo makes Mongo accessor
func NewMongo(address []string, password string, dbName string, delay int) (res *Mongo, err error) {
	log.Printf("[INFO] make new mongo server with ip=%v, db=%s, delay=%dsecs", address, dbName, delay)
	mg, err := mongo.NewServer(
		mgo.DialInfo{
			Addrs:    address,
			AppName:  "dkll",
			Database: dbName,
			Username: "root",
			Password: password,
			Timeout:  time.Minute,
		},
		mongo.ServerParams{
			Delay:           delay,
			ConsistencyMode: mgo.Monotonic,
		},
	)

	return &Mongo{mongo.NewConnection(mg, dbName, "msgs")}, nil
}

// Publish inserts buffer to mongo
func (m *Mongo) Publish(records []core.LogEntry) (err error) {
	recs := make([]interface{}, len(records))
	for i, v := range records {
		recs[i] = v
	}
	err = m.WithCollection(func(coll *mgo.Collection) error {
		return coll.Insert(recs...)
	})
	return err
}

func (m *Mongo) LastPublished() (entry core.LogEntry, err error) {
	err = m.WithCollection(func(coll *mgo.Collection) error {
		return coll.Find(bson.M{}).Sort("-_id").Limit(1).One(&entry)
	})
	return entry, err
}

func (m *Mongo) Find(req core.Request) ([]core.LogEntry, error) {

	query, err := m.makeQuery(req)
	if err != nil {
		return nil, err
	}

	var result []core.LogEntry
	err = m.WithCollection(func(coll *mgo.Collection) error {
		return coll.Find(query).Sort("+_id").All(&result)
	})
	if err != nil {
		return nil, errors.Wrapf(err, "can't get records for %+v", req)
	}
	return result, nil

}

func (m *Mongo) makeQuery(req core.Request) (b bson.M, err error) {

	query := bson.M{"_id": bson.M{"$gt": m.getBid(req.LastID)}, "ts": bson.M{"$gte": req.FromTS, "$lt": req.ToTS}}

	if req.Containers != nil && len(req.Containers) > 0 {
		query["container"] = bson.M{"$in": m.convertListWithRegex(req.Containers)}
	}

	if req.Hosts != nil && len(req.Hosts) > 0 {
		query["host"] = bson.M{"$in": m.convertListWithRegex(req.Hosts)}
	}

	if req.Excludes != nil && len(req.Excludes) > 0 {
		if val, found := query["container"]; found {
			val.(bson.M)["$nin"] = m.convertListWithRegex(req.Excludes)
		} else {
			query["container"] = bson.M{"$nin": m.convertListWithRegex(req.Excludes)}
		}
	}

	return query, nil
}

func (m *Mongo) convertListWithRegex(elems []string) []interface{} {
	var result []interface{}
	for _, elem := range elems {
		if strings.HasPrefix(elem, "/") && strings.HasSuffix(elem, "/") {
			result = append(result, bson.RegEx{Pattern: elem[1 : len(elem)-1], Options: ""})
		} else {
			result = append(result, elem)
		}
	}
	return result
}

func (m *Mongo) getBid(id string) bson.ObjectId {
	bid := bson.ObjectId("000000000000")
	if id != "0" && bson.IsObjectIdHex(id) {
		bid = bson.ObjectIdHex(id)
	}
	return bid
}

// init collection, make/ensure indexes
func (m *Mongo) init(collection string) error {
	log.Printf("[INFO] create collection %s", collection)

	indexes := []mgo.Index{
		{Key: []string{"host", "container", "ts"}},
		{Key: []string{"ts", "host", "container"}},
		{Key: []string{"container", "ts"}},
	}

	err := m.WithDB(func(dbase *mgo.Database) error {
		coll := dbase.C(collection)
		e := coll.Create(&mgo.CollectionInfo{ForceIdIndex: true, Capped: true, MaxBytes: 50000000000, MaxDocs: 500000000})
		if e != nil {
			return e
		}
		for _, index := range indexes {
			if err := coll.EnsureIndex(index); err != nil {
				log.Printf("[WARN] can't insure index %v, %v", index, err)
			}
		}
		return nil
	})

	return err
}