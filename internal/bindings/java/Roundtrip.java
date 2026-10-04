import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.flanksource.uir.WireCodec;

public final class Roundtrip {
    public static void main(String[] args) throws Exception {
        ObjectMapper mapper = new ObjectMapper();
        WireCodec codec = new WireCodec();
        ArrayNode result = mapper.createArrayNode();
        for (JsonNode input : mapper.readTree(System.in)) {
            JsonNode value;
            switch (input.get("hierarchy").textValue()) {
                case "Node": value = codec.encodeNode(codec.parseNode(input.get("value"))); break;
                case "Statement": value = codec.encodeStatement(codec.parseStatement(input.get("value"))); break;
                default: throw new IllegalArgumentException("Unknown hierarchy: " + input.get("hierarchy"));
            }
            ObjectNode output = input.deepCopy();
            output.set("value", value);
            result.add(output);
        }
        mapper.writeValue(System.out, result);
    }
}
