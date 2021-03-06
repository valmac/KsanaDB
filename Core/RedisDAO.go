package KsanaDB

import (
    redis "github.com/garyburd/redigo/redis"
    "log"
    "fmt"
    "time"
    "errors"
)

var pool *redis.Pool 
var maxPipeline int
type getClientFunc func() redis.Conn

func getClient() redis.Conn {
    return pool.Get()   
}

var clientFunction = getClient

func InitRedis(network, address string)  {
    maxPipeline = 8000 // too many  pipeline will get fewer data
    pool = &redis.Pool{                                                                                                    
        MaxIdle:     80,
        MaxActive: 12000,
        IdleTimeout: 240 * time.Second,                          
        Dial: func() (redis.Conn, error) {                                                                        
            c, err := redis.Dial(network, address)                                                                   
                if err != nil { 
                    log.Fatalf(err.Error())
                    return nil, err                                                                               
                }                                                                                                 
                return c, err                                                                                         
        },                                                                                                        
        TestOnBorrow: func(c redis.Conn, t time.Time) error { 
                  _, err := c.Do("PING") 
                  if err != nil { 
                      log.Fatalf(err.Error())
                      return err 
                  }
                  return nil 
        },                                                                                                           
    } 
}  

func BulkSetTimeSeries(metrics string, input []interface{}) (int, error) {
    client := clientFunction()
    defer client.Close()
    return redis.Int(client.Do("ZADD", redis.Args{metrics}.AddFlat(input)...))
}

func SetTimeSeries(metrics string, value string, time int64) (int, error) {
    client := clientFunction()
    defer client.Close()
    input := []interface{}{}
    input = append(input,time)
    input = append(input,value)
    return redis.Int(client.Do("ZADD", redis.Args{metrics}.AddFlat(input)...))
}

func queryTimeSeries(prefix string, name string, start int64, stop int64) ([]string, error) {
    client := clientFunction()
    defer client.Close()
    cmds := getTimeseriesQueryCmd(prefix, name, start, stop)

    if len(cmds) > maxPipeline {
        return []string{}, errors.New(fmt.Sprintf("time %d - %d , query %d over upper limit duration days %d", start, stop, len(cmds), maxPipeline))    
    }

    for _, cmd := range cmds {

        client.Send("ZRANGEBYSCORE", redis.Args{cmd["keyName"], cmd["from"], cmd["to"]}...)
    }
    client.Flush()
    
    ret := []string{}
    for _,_ = range cmds {
        p, err := redis.Strings(client.Receive())
        if err != nil || len(p) == 0{
            continue
        }

        ret = append(ret, p...)
    }
    return ret, nil
}


func setTags(prefix string, metrics string, tags []string) (string) {
    //TODO: call function
    client := clientFunction()
    defer client.Close()
    hashName := prefix + metrics + "\tTagHash"
    listName := prefix + metrics + "\tTagList"

    args := []string{}

    args = append(args, tags...)
    args = append(args, hashName)
    args = append(args, listName)

    scriptArgs := make([]interface{}, len(args))
    for i, v := range args {
            scriptArgs[i] = v
    }

    s := getLuaScript("setTag")
    script := redis.NewScript(len(tags), s)

    result, err := redis.String(script.Do(client, scriptArgs...))
    if err != nil {
        log.Println(err)    
    }
    return result
} 

func getTags(prefix string, metrics string, target string, keyName string) (string) {
    client := clientFunction()
    defer client.Close()
    listName := prefix + metrics + "\tTagList"
    s := getLuaScript("getTag")
    script := redis.NewScript(0, s)

    result, err := redis.String(script.Do(client, listName, target, keyName))
    if err != nil {
        log.Println(err)    
    }
    return result
} 

func getSeqByKV(prefix string, metrics string, filterKeyValue []string) ([]string, error) {
    client := clientFunction()
    defer client.Close()
    hashName := prefix + metrics + "\tTagHash"
    return redis.Strings(client.Do("HMGET", redis.Args{hashName}.AddFlat(filterKeyValue)...))
}

func getMetric(prefix string) (string, error) {
    client := clientFunction()
    defer client.Close()
    dbName := prefix
    s := getLuaScript("getMetric")
    script := redis.NewScript(0, s)

    result, err := redis.String(script.Do(client, dbName))
    if err != nil {
        log.Println(err)    
    }
    return result, err
} 

func getMetricKeys(prefix string, metrics string) ([]string, error) {
    client := clientFunction()
    defer client.Close()
    name := prefix + metrics + "\t"
    s := getLuaScript("getMetricKeys")
    script := redis.NewScript(0, s)

    ret, err := redis.Strings(script.Do(client, name))
    if err != nil {
        log.Println(err)   
        return []string{}, err
    }
    return ret, err
}

func deleteKeys(keys []string) {
    client := clientFunction()
    defer client.Close()
    for _, key := range keys {
        client.Send("DEL", redis.Args{key}...)
    }
    client.Flush()
}

func Close() {
    pool.Close()
}
