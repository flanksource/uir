package com.flanksource.uir;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;
import com.networknt.schema.JsonSchema;
import com.networknt.schema.JsonSchemaFactory;
import com.networknt.schema.SchemaLocation;
import com.networknt.schema.SchemaValidatorsConfig;
import com.networknt.schema.SpecVersion;
import com.networknt.schema.ValidationMessage;
import java.util.Set;

public final class WireCodec {
    private final ObjectMapper mapper = new ObjectMapper().registerModule(new JavaTimeModule());
    private final JsonSchema nodeSchema;
    private final JsonSchema statementSchema;

    public WireCodec() {
        mapper.setDefaultPropertyInclusion(JsonInclude.Value.construct(JsonInclude.Include.NON_NULL, JsonInclude.Include.ALWAYS));
        JsonSchemaFactory factory = JsonSchemaFactory.getInstance(SpecVersion.VersionFlag.V202012);
        SchemaValidatorsConfig config = SchemaValidatorsConfig.builder().formatAssertionsEnabled(true).build();
        nodeSchema = factory.getSchema(SchemaLocation.of("classpath:uir.schema.json#/$defs/Node"), config);
        statementSchema = factory.getSchema(SchemaLocation.of("classpath:uir.schema.json#/$defs/Statement"), config);
    }

    public Node parseNode(JsonNode value) {
        validate(nodeSchema, value);
        return decode(value, Node.class);
    }

    public Statement parseStatement(JsonNode value) {
        validate(statementSchema, value);
        return decode(value, Statement.class);
    }

    public JsonNode encodeNode(Node value) {
        JsonNode encoded = mapper.valueToTree(value);
        validate(nodeSchema, encoded);
        return encoded;
    }

    public JsonNode encodeStatement(Statement value) {
        JsonNode encoded = mapper.valueToTree(value);
        validate(statementSchema, encoded);
        return encoded;
    }

    private <T> T decode(JsonNode value, Class<T> type) {
        try {
            return mapper.treeToValue(value, type);
        } catch (JsonProcessingException error) {
            throw new IllegalArgumentException("Invalid UIR " + type.getSimpleName(), error);
        }
    }

    private void validate(JsonSchema schema, JsonNode value) {
        Set<ValidationMessage> errors = schema.validate(value);
        if (!errors.isEmpty()) {
            throw new IllegalArgumentException("Invalid UIR " + value + ": " + errors);
        }
    }
}
