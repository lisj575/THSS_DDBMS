package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"../labgob"
	"../labrpc"
	"github.com/google/uuid"
)

// Cluster consists of a group of nodes to manage distributed tables defined in models/table.go.
// The Cluster object itself can also be viewed as the only coordinator of a cluster, which means client requests
// should go through it instead of the nodes.
// Of course, it is possible to make any of the nodes a coordinator and to make the cluster decentralized. You are
// welcomed to make such changes and may earn some extra points.
type Cluster struct {
	// the identifiers of each node, we use simple numbers like "1,2,3" to register the nodes in the network
	// needless to say, each identifier should be unique
	nodeIds      []string
	tableName2id map[string][]string
	// how many rules does this table have (How many copies of this table can a node have at most)
	tableName2num map[string]int
	// the network that the cluster works on. It is not actually using the network interface, but a network simulator
	// using SEDA (google it if you have not heard about it), which allows us (and you) to inject some network failures
	// during tests. Do remember that network failures should always be concerned in a distributed environment.
	network *labrpc.Network
	// the Name of the cluster, also used as a network address of the cluster coordinator in the network above
	Name string
}

// NewCluster creates a Cluster with the given number of nodes and register the nodes to the given network.
// The created cluster will be named with the given one, which will used when a client wants to connect to the cluster
// and send requests to it. WARNING: the given name should not be like "Node0", "Node1", ..., as they will conflict
// with some predefined names.
// The created nodes are identified by simple numbers starting from 0, e.g., if we have 3 nodes, the identifiers of the
// three nodes will be "Node0", "Node1", and "Node2".
// Each node is bound to a server in the network which follows the same naming rule, for the example above, the three
// nodes will be bound to  servers "Node0", "Node1", and "Node2" respectively.
// In practice, we may mix the usages of terms "Node" and "Server", both of them refer to a specific machine, while in
// the lab, a "Node" is responsible for processing distributed affairs but a "Server" simply receives messages from the
// net work.
func NewCluster(nodeNum int, network *labrpc.Network, clusterName string) *Cluster {
	labgob.Register(TableSchema{})
	labgob.Register(Row{})
	labgob.Register(Predicate{})
	labgob.Register(json.Number(""))
	tableName2id := make(map[string][]string)
	tableName2num := make(map[string]int)
	nodeIds := make([]string, nodeNum)
	nodeNamePrefix := "Node"
	for i := 0; i < nodeNum; i++ {
		// identify the nodes with "Node0", "Node1", ...
		node := NewNode(nodeNamePrefix + strconv.Itoa(i))
		nodeIds[i] = node.Identifier
		// use go reflection to extract the methods in a Node object and make them as a service.
		// a service can be viewed as a list of methods that a server provides.
		// due to the limitation of the framework, the extracted method must only have two parameters, and the first one
		// is the actual argument list, while the second one is the reference to the result.
		// NOTICE, a REFERENCE should be passed to the method instead of a value
		nodeService := labrpc.MakeService(node)
		// create a server, a server is responsible for receiving requests and dispatching them
		server := labrpc.MakeServer()
		// add the service to the server so the server can provide the services
		server.AddService(nodeService)
		// register the server to the network as "Node0", "Node1", ...
		network.AddServer(nodeIds[i], server)
	}

	// create a cluster with the nodes and the network
	c := &Cluster{nodeIds: nodeIds, network: network, Name: clusterName, tableName2id: tableName2id, tableName2num: tableName2num}
	// create a coordinator for the cluster to receive external requests, the steps are similar to those above.
	// notice that we use the reference of the cluster as the name of the coordinator server,
	// and the names can be more than strings.
	clusterService := labrpc.MakeService(c)
	server := labrpc.MakeServer()
	server.AddService(clusterService)
	network.AddServer(clusterName, server)
	return c
}

// SayHello is an example to show how the coordinator communicates with other nodes in the cluster.
// Any method that can be accessed by network clients should have EXACTLY TWO parameters, while the first one is the
// actual parameter desired by the method (can be a list if there are more than one desired parameters), and the second
// one is a reference to the return value. The caller must ensure that the reference is valid (not nil).
func (c *Cluster) SayHello(visitor string, reply *string) {
	endNamePrefix := "InternalClient"
	for _, nodeId := range c.nodeIds {
		// create a client (end) to each node
		// the name of the client should be unique, so we use the name of each node for it
		endName := endNamePrefix + nodeId
		end := c.network.MakeEnd(endName)
		// connect the client to the node
		c.network.Connect(endName, nodeId)
		// a client should be enabled before being used
		c.network.Enable(endName, true)
		// call method on that node
		argument := visitor
		reply := ""
		// the first parameter is the name of the method to be called, recall that we use the reference of
		// a Node object to create a service, so the first part of the parameter will be the class name "Node", and as
		// we want to call the method SayHello(), so the second part is "SayHello", and the two parts are separated by
		// a dot
		end.Call("Node.SayHello", argument, &reply)
		fmt.Println(reply)
	}
	*reply = fmt.Sprintf("Hello %s, I am the coordinator of %s", visitor, c.Name)
}

// Join all tables in the given list using NATURAL JOIN (join on the common columns), and return the joined result
// as a list of rows and set it to reply.
func (c *Cluster) Join(tableNames []string, reply *Dataset) {

	// 开始根据节点连接数据
	result_rows := make([]Row, 0)
	newColumns := make([]ColumnSchema, 0)
	same_columns1 := make([]int, 0)
	same_columns2 := make([]int, 0)
	table1_columns := make([]ColumnSchema, 0)
	table2_columns := make([]ColumnSchema, 0)
	if len(tableNames) >= 2 {

		// 获取完整的表头
		tableName1 := tableNames[0]
		tableName2 := tableNames[1]
		table1_ids := c.tableName2id[tableName1]
		table2_ids := c.tableName2id[tableName2]
		endNamePrefix := "InternalClient"
		for _, nodeId := range c.nodeIds {
			endName := endNamePrefix + nodeId
			end := c.network.MakeEnd(endName)
			c.network.Connect(endName, nodeId)
			c.network.Enable(endName, true)
			if len(table1_columns) != 0 && len(table2_columns) != 0 {
				break
			}
			if len(table1_columns) == 0 {
				for i := 0; i < c.tableName2num[tableName1]; i++ {
					end.Call("Node.GetFullSchema", tableName1+"|"+strconv.Itoa(i), &table1_columns)
				}
			}
			if len(table2_columns) == 0 {
				for i := 0; i < c.tableName2num[tableName2]; i++ {
					end.Call("Node.GetFullSchema", tableName2+"|"+strconv.Itoa(i), &table2_columns)
				}
			}
		}

		createJoinSchema([]interface{}{table1_columns, table2_columns}, &newColumns, &same_columns1, &same_columns2)

		if len(same_columns1) != 0 {
			need_join := true
			for _, id1 := range table1_ids {
				lineOfTable1 := getLineByid(c, tableName1, id1, table1_columns)
				if lineOfTable1.Schema.TableName == "" {
					continue
				}
				for _, id2 := range table2_ids {
					lineOfTable2 := getLineByid(c, tableName2, id2, table2_columns)
					if lineOfTable2.Schema.TableName == "" {
						continue
					}
					subRow1 := lineOfTable1.Rows[0]
					subRow2 := lineOfTable2.Rows[0]
					join_data := true
					for i := 0; i < len(same_columns1); i++ {
						if subRow1[same_columns1[i]] != subRow2[same_columns2[i]] {
							join_data = false
							break
						}
					}
					if join_data == false {
						continue
					}
					ind := 0
					for i, val := range subRow2 {
						if i >= len(same_columns2) {
							subRow1 = append(subRow1, subRow2[i:]...)
							break
						} else {
							if i != same_columns2[ind] {
								subRow1 = append(subRow1, val)
							} else {
								ind++
							}
						}
					}
					result_rows = append(result_rows, subRow1)
				}
				if need_join == false {
					break
				}
			}
		}
	}

	result := Dataset{}
	result.Schema = TableSchema{TableName: "", ColumnSchemas: newColumns}
	result.Rows = result_rows
	*reply = result
}

func createJoinSchema(args []interface{}, newColumns *[]ColumnSchema, same_columns1 *[]int, same_columns2 *[]int) {
	table_schemas1 := args[0].([]ColumnSchema)
	table_schemas2 := args[1].([]ColumnSchema)

	// 获取相同列的索引
	sameColumns1 := make([]int, 0)
	sameColumns2 := make([]int, 0)

	for ind1, col1 := range table_schemas1 {
		for ind2, col2 := range table_schemas2 {
			if col1 == col2 {
				sameColumns1 = append(sameColumns1, ind1)
				sameColumns2 = append(sameColumns2, ind2)
				break
			}
		}
	}
	// 构建新的表头
	result_columns := table_schemas1 // 添加表一表头
	// 添加表2的表头
	i := 0
	same_size := len(sameColumns2)
	for ind1, col1 := range table_schemas2 {
		if i < same_size && ind1 == sameColumns2[i] {
			i++
			continue
		}
		result_columns = append(result_columns, col1)
	}
	*newColumns = result_columns
	*same_columns1 = sameColumns1
	*same_columns2 = sameColumns2
}

func getLineByid(c *Cluster, tableName string, id string, fullSchema []ColumnSchema) Dataset {
	endNamePrefix := "InternalClient"

	resultColumns := make([]ColumnSchema, 0)
	var resultRow Row
	Rows := make([]Row, 1)
	ret_tablename := ""
	for _, nodeId := range c.nodeIds {
		endName := endNamePrefix + nodeId
		end := c.network.MakeEnd(endName)
		c.network.Connect(endName, nodeId)
		c.network.Enable(endName, true)

		line := Dataset{}
		find := false
		for i := 0; i < c.tableName2num[tableName]; i++ {
			end.Call("Node.ScanLineData", []interface{}{tableName+"|"+strconv.Itoa(i), id}, &line)
			if line.Schema.TableName != "" && len(line.Rows) > 0 && len(line.Rows[0]) > 0 {
				find = true
				break
			}
		}
		if !find {
			continue
		}

		ret_tablename = tableName
		resultColumns = append(resultColumns, line.Schema.ColumnSchemas[1:]...)
		resultRow = append(resultRow, line.Rows[0][1:]...)
	}

	for _, col1 := range fullSchema {
		for j, col2 := range resultColumns {
			if col1 == col2 {
				Rows[0] = append(Rows[0], resultRow[j])
				break
			}
		}
	}
	resultSet := Dataset{}
	if len(Rows) > 0 {
		resultSet.Schema = TableSchema{TableName: ret_tablename, ColumnSchemas: fullSchema}
		resultSet.Rows = Rows
	}

	return resultSet
}

func (c *Cluster) BuildTable(params []interface{}, reply *string) {
	schema := params[0].(TableSchema)
	schema.ColumnSchemas = append(schema.ColumnSchemas, ColumnSchema{Name: "id", DataType: TypeString})
	rules := make(map[string]Rule)
	c.tableName2id[schema.TableName] = make([]string, 0)

	decoder := json.NewDecoder(bytes.NewReader(params[1].([]byte)))
	decoder.UseNumber()
	decoder.Decode(&rules)
	c.tableName2num[schema.TableName] = len(rules)

	nodeNamePrefix := "Node"
	endNamePrefix := "InternalClient"
	i := 0
	for key, value := range rules {
		ts := &TableSchema{TableName: schema.TableName + "|" + strconv.Itoa(i), ColumnSchemas: make([]ColumnSchema, 0)}
		i++
		ts.ColumnSchemas = append(ts.ColumnSchemas, ColumnSchema{Name: "id", DataType: TypeString})
		for _, columnName := range value.Column {
			for _, cs := range schema.ColumnSchemas {
				if cs.Name == columnName {
					ts.ColumnSchemas = append(ts.ColumnSchemas, cs)
					break
				}
			}
		}

		nodeIds := strings.Split(key, "|")
		for _, nodeId := range nodeIds {
			nodeName := nodeNamePrefix + nodeId
			endName := endNamePrefix + nodeName
			end := c.network.MakeEnd(endName)
			c.network.Connect(endName, nodeName)
			c.network.Enable(endName, true)
			end.Call("Node.RPCCreateTable", []interface{}{ts, value.Predicate, schema}, reply)
			if (*reply)[0] != '0' {
				return
			}
		}
	}
}

func (c *Cluster) FragmentWrite(params []interface{}, reply *string) {
	tableName := params[0].(string)
	row := params[1].(Row)
	uuid := uuid.New().String()
	c.tableName2id[tableName] = append(c.tableName2id[tableName], uuid)
	row = append(row, uuid)
	*reply = "1 Not Insert"

	endNamePrefix := "InternalClient"
	for _, nodeId := range c.nodeIds {
		endName := endNamePrefix + nodeId
		end := c.network.MakeEnd(endName)
		c.network.Connect(endName, nodeId)
		c.network.Enable(endName, true)
		replyMsg := ""
		for i := 0; i < c.tableName2num[tableName]; i++ {
			end.Call("Node.RPCInsert", []interface{}{tableName + "|" + strconv.Itoa(i), row}, &replyMsg)
			if replyMsg[0] == '0' {
				*reply = "0 OK"
			}
		}
	}
}
