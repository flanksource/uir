import com.fasterxml.jackson.databind.ObjectMapper;
import com.flanksource.uir.Node;
import com.flanksource.uir.WireCodec;

public final class Consumer {
    public static void main(String[] args) throws Exception {
        WireCodec codec = new WireCodec();
        ObjectMapper json = new ObjectMapper();
        Node target = codec.parseNode(json.readTree("{\"node_kind\":\"ref\",\"method\":\"Run\"}"));
        System.out.println(json.writeValueAsString(codec.encodeNode(target)));
    }
}
