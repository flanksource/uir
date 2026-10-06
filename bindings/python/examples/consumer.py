import json

from uir import encode_node, parse_node


target = parse_node({"node_kind": "ref", "method": "Run"})
print(json.dumps(encode_node(target)))
