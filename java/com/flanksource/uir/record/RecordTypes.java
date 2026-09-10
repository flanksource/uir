package com.flanksource.uir.record;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.flanksource.archunit.uir.enums.UIREnums.*;
import com.flanksource.uir.core.*;
import java.util.List;
import java.util.Map;

/**
 * Container for all record-related types
 */
public class RecordTypes {

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ValidationValue {
        @JsonProperty("stringValue")
        private String stringValue;

        @JsonProperty("numberValue")
        private Double numberValue;

        @JsonProperty("boolValue")
        private Boolean boolValue;

        @JsonProperty("arrayValue")
        private List<ValidationValue> arrayValue;

        @JsonProperty("objectValue")
        private Map<String, ValidationValue> objectValue;

        @JsonProperty("code")
        private String code;

        public ValidationValue() {}

        public String getStringValue() { return stringValue; }
        public void setStringValue(String stringValue) { this.stringValue = stringValue; }

        public Double getNumberValue() { return numberValue; }
        public void setNumberValue(Double numberValue) { this.numberValue = numberValue; }

        public Boolean getBoolValue() { return boolValue; }
        public void setBoolValue(Boolean boolValue) { this.boolValue = boolValue; }

        public List<ValidationValue> getArrayValue() { return arrayValue; }
        public void setArrayValue(List<ValidationValue> arrayValue) { this.arrayValue = arrayValue; }

        public Map<String, ValidationValue> getObjectValue() { return objectValue; }
        public void setObjectValue(Map<String, ValidationValue> objectValue) { this.objectValue = objectValue; }

        public String getCode() { return code; }
        public void setCode(String code) { this.code = code; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ValidationRuleOptions {
        @JsonProperty("message")
        private String message;

        @JsonProperty("groups")
        private List<String> groups;

        @JsonProperty("always")
        private Boolean always;

        @JsonProperty("each")
        private Boolean each;

        @JsonProperty("context")
        private Map<String, ValidationValue> context;

        public ValidationRuleOptions() {}

        public String getMessage() { return message; }
        public void setMessage(String message) { this.message = message; }

        public List<String> getGroups() { return groups; }
        public void setGroups(List<String> groups) { this.groups = groups; }

        public Boolean getAlways() { return always; }
        public void setAlways(Boolean always) { this.always = always; }

        public Boolean getEach() { return each; }
        public void setEach(Boolean each) { this.each = each; }

        public Map<String, ValidationValue> getContext() { return context; }
        public void setContext(Map<String, ValidationValue> context) { this.context = context; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ValidationRule {
        @JsonProperty("provider")
        private String provider;

        @JsonProperty("name")
        private String name;

        @JsonProperty("args")
        private List<ValidationValue> args;

        @JsonProperty("options")
        private ValidationRuleOptions options;

        public ValidationRule() {}

        public String getProvider() { return provider; }
        public void setProvider(String provider) { this.provider = provider; }

        public String getName() { return name; }
        public void setName(String name) { this.name = name; }

        public List<ValidationValue> getArgs() { return args; }
        public void setArgs(List<ValidationValue> args) { this.args = args; }

        public ValidationRuleOptions getOptions() { return options; }
        public void setOptions(ValidationRuleOptions options) { this.options = options; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class GeneratorHint {
        @JsonProperty("language")
        private String language;

        @JsonProperty("framework")
        private String framework;

        @JsonProperty("properties")
        private Map<String, ValidationValue> properties;

        public GeneratorHint() {}

        public String getLanguage() { return language; }
        public void setLanguage(String language) { this.language = language; }

        public String getFramework() { return framework; }
        public void setFramework(String framework) { this.framework = framework; }

        public Map<String, ValidationValue> getProperties() { return properties; }
        public void setProperties(Map<String, ValidationValue> properties) { this.properties = properties; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class FieldValidation {
        @JsonProperty("minLength")
        private Integer minLength;

        @JsonProperty("maxLength")
        private Integer maxLength;

        @JsonProperty("maxItems")
        private Integer maxItems;

        @JsonProperty("minItems")
        private Integer minItems;

        @JsonProperty("minProperties")
        private Integer minProperties;

        @JsonProperty("maxProperties")
        private Integer maxProperties;

        @JsonProperty("uniqueItems")
        private Boolean uniqueItems;

        @JsonProperty("sortedItems")
        private Boolean sortedItems;

        @JsonProperty("regex")
        private String regex;

        @JsonProperty("enum")
        private List<String> enumValues;

        @JsonProperty("min")
        private Double min;

        @JsonProperty("minExclusive")
        private Double minExclusive;

        @JsonProperty("maxExclusive")
        private Double maxExclusive;

        @JsonProperty("max")
        private Double max;

        @JsonProperty("minDate")
        private String minDate;

        @JsonProperty("maxDate")
        private String maxDate;

        @JsonProperty("required")
        private Boolean required;

        @JsonProperty("unique")
        private Boolean unique;

        @JsonProperty("nonEmpty")
        private Boolean nonEmpty;

        @JsonProperty("expressions")
        private List<Expression> expressions;

        @JsonProperty("rules")
        private List<ValidationRule> rules;

        @JsonProperty("hints")
        private List<GeneratorHint> hints;

        public FieldValidation() {}

        // Getters and setters
        public Integer getMinLength() { return minLength; }
        public void setMinLength(Integer minLength) { this.minLength = minLength; }

        public Integer getMaxLength() { return maxLength; }
        public void setMaxLength(Integer maxLength) { this.maxLength = maxLength; }

        public Integer getMaxItems() { return maxItems; }
        public void setMaxItems(Integer maxItems) { this.maxItems = maxItems; }

        public Integer getMinItems() { return minItems; }
        public void setMinItems(Integer minItems) { this.minItems = minItems; }

        public Integer getMinProperties() { return minProperties; }
        public void setMinProperties(Integer minProperties) { this.minProperties = minProperties; }

        public Integer getMaxProperties() { return maxProperties; }
        public void setMaxProperties(Integer maxProperties) { this.maxProperties = maxProperties; }

        public Boolean getUniqueItems() { return uniqueItems; }
        public void setUniqueItems(Boolean uniqueItems) { this.uniqueItems = uniqueItems; }

        public Boolean getSortedItems() { return sortedItems; }
        public void setSortedItems(Boolean sortedItems) { this.sortedItems = sortedItems; }

        public String getRegex() { return regex; }
        public void setRegex(String regex) { this.regex = regex; }

        public List<String> getEnumValues() { return enumValues; }
        public void setEnumValues(List<String> enumValues) { this.enumValues = enumValues; }

        public Double getMin() { return min; }
        public void setMin(Double min) { this.min = min; }

        public Double getMinExclusive() { return minExclusive; }
        public void setMinExclusive(Double minExclusive) { this.minExclusive = minExclusive; }

        public Double getMaxExclusive() { return maxExclusive; }
        public void setMaxExclusive(Double maxExclusive) { this.maxExclusive = maxExclusive; }

        public Double getMax() { return max; }
        public void setMax(Double max) { this.max = max; }

        public String getMinDate() { return minDate; }
        public void setMinDate(String minDate) { this.minDate = minDate; }

        public String getMaxDate() { return maxDate; }
        public void setMaxDate(String maxDate) { this.maxDate = maxDate; }

        public Boolean getRequired() { return required; }
        public void setRequired(Boolean required) { this.required = required; }

        public Boolean getUnique() { return unique; }
        public void setUnique(Boolean unique) { this.unique = unique; }

        public Boolean getNonEmpty() { return nonEmpty; }
        public void setNonEmpty(Boolean nonEmpty) { this.nonEmpty = nonEmpty; }

        public List<Expression> getExpressions() { return expressions; }
        public void setExpressions(List<Expression> expressions) { this.expressions = expressions; }

        public List<ValidationRule> getRules() { return rules; }
        public void setRules(List<ValidationRule> rules) { this.rules = rules; }

        public List<GeneratorHint> getHints() { return hints; }
        public void setHints(List<GeneratorHint> hints) { this.hints = hints; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class RecordField {
        @JsonProperty("name")
        private String name;

        @JsonProperty("label")
        private String label;

        @JsonProperty("fieldType")
        private RecordFieldType fieldType;

        @JsonProperty("defaultValue")
        private TypedValue defaultValue;

        @JsonProperty("readOnly")
        private Boolean readOnly;

        @JsonProperty("writeOnly")
        private Boolean writeOnly;

        @JsonProperty("validation")
        private FieldValidation validation;

        @JsonProperty("visibility")
        private Visibility visibility;

        @JsonProperty("annotations")
        private List<Annotation> annotations;

        @JsonProperty("comments")
        private List<Comment> comments;

        @JsonProperty("location")
        private Location location;

        public RecordField() {}

        public String getName() { return name; }
        public void setName(String name) { this.name = name; }

        public String getLabel() { return label; }
        public void setLabel(String label) { this.label = label; }

        public RecordFieldType getFieldType() { return fieldType; }
        public void setFieldType(RecordFieldType fieldType) { this.fieldType = fieldType; }

        public TypedValue getDefaultValue() { return defaultValue; }
        public void setDefaultValue(TypedValue defaultValue) { this.defaultValue = defaultValue; }

        public Boolean getReadOnly() { return readOnly; }
        public void setReadOnly(Boolean readOnly) { this.readOnly = readOnly; }

        public Boolean getWriteOnly() { return writeOnly; }
        public void setWriteOnly(Boolean writeOnly) { this.writeOnly = writeOnly; }

        public FieldValidation getValidation() { return validation; }
        public void setValidation(FieldValidation validation) { this.validation = validation; }

        public Visibility getVisibility() { return visibility; }
        public void setVisibility(Visibility visibility) { this.visibility = visibility; }

        public List<Annotation> getAnnotations() { return annotations; }
        public void setAnnotations(List<Annotation> annotations) { this.annotations = annotations; }

        public List<Comment> getComments() { return comments; }
        public void setComments(List<Comment> comments) { this.comments = comments; }

        public Location getLocation() { return location; }
        public void setLocation(Location location) { this.location = location; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class RecordValidation {
        @JsonProperty("uniqueFields")
        private List<String> uniqueFields;

        @JsonProperty("minRecords")
        private Integer minRecords;

        @JsonProperty("maxRecords")
        private Integer maxRecords;

        @JsonProperty("anyOf")
        private List<String> anyOf;

        @JsonProperty("oneOf")
        private List<String> oneOf;

        @JsonProperty("allOf")
        private List<String> allOf;

        @JsonProperty("noneOf")
        private List<String> noneOf;

        @JsonProperty("expressions")
        private List<Expression> expressions;

        @JsonProperty("hints")
        private List<GeneratorHint> hints;

        public RecordValidation() {}

        public List<String> getUniqueFields() { return uniqueFields; }
        public void setUniqueFields(List<String> uniqueFields) { this.uniqueFields = uniqueFields; }

        public Integer getMinRecords() { return minRecords; }
        public void setMinRecords(Integer minRecords) { this.minRecords = minRecords; }

        public Integer getMaxRecords() { return maxRecords; }
        public void setMaxRecords(Integer maxRecords) { this.maxRecords = maxRecords; }

        public List<String> getAnyOf() { return anyOf; }
        public void setAnyOf(List<String> anyOf) { this.anyOf = anyOf; }

        public List<String> getOneOf() { return oneOf; }
        public void setOneOf(List<String> oneOf) { this.oneOf = oneOf; }

        public List<String> getAllOf() { return allOf; }
        public void setAllOf(List<String> allOf) { this.allOf = allOf; }

        public List<String> getNoneOf() { return noneOf; }
        public void setNoneOf(List<String> noneOf) { this.noneOf = noneOf; }

        public List<Expression> getExpressions() { return expressions; }
        public void setExpressions(List<Expression> expressions) { this.expressions = expressions; }

        public List<GeneratorHint> getHints() { return hints; }
        public void setHints(List<GeneratorHint> hints) { this.hints = hints; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class RecordReference {
        @JsonProperty("name")
        private String name;

        @JsonProperty("mapping")
        private String mapping;

        /* Temporarily commented out - package mismatch
        @JsonProperty("recordReferenceType")
        private RecordReferenceType recordReferenceType;
        */

        public RecordReference() {}

        public String getName() { return name; }
        public void setName(String name) { this.name = name; }

        public String getMapping() { return mapping; }
        public void setMapping(String mapping) { this.mapping = mapping; }

        /* Temporarily commented out - package mismatch
        public RecordReferenceType getRecordReferenceType() { return recordReferenceType; }
        public void setRecordReferenceType(RecordReferenceType recordReferenceType) {
            this.recordReferenceType = recordReferenceType;
        }
        */
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ASTRecord {
        @JsonProperty("name")
        private String name;

        @JsonProperty("extends")
        private List<ASTRecord> extendsRecords;

        @JsonProperty("description")
        private String description;

        @JsonProperty("recordType")
        private RecordType recordType;

        @JsonProperty("fields")
        private List<RecordField> fields;

        @JsonProperty("examples")
        private List<String> examples;

        @JsonProperty("validation")
        private RecordValidation validation;

        @JsonProperty("references")
        private List<RecordReference> references;

        @JsonProperty("location")
        private Location location;

        public ASTRecord() {}

        public String getName() { return name; }
        public void setName(String name) { this.name = name; }

        public List<ASTRecord> getExtendsRecords() { return extendsRecords; }
        public void setExtendsRecords(List<ASTRecord> extendsRecords) { this.extendsRecords = extendsRecords; }

        public String getDescription() { return description; }
        public void setDescription(String description) { this.description = description; }

        public RecordType getRecordType() { return recordType; }
        public void setRecordType(RecordType recordType) { this.recordType = recordType; }

        public List<RecordField> getFields() { return fields; }
        public void setFields(List<RecordField> fields) { this.fields = fields; }

        public List<String> getExamples() { return examples; }
        public void setExamples(List<String> examples) { this.examples = examples; }

        public RecordValidation getValidation() { return validation; }
        public void setValidation(RecordValidation validation) { this.validation = validation; }

        public List<RecordReference> getReferences() { return references; }
        public void setReferences(List<RecordReference> references) { this.references = references; }

        public Location getLocation() { return location; }
        public void setLocation(Location location) { this.location = location; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ASTError {
        @JsonProperty("name")
        private String name;

        @JsonProperty("errorType")
        private ASTErrorType errorType;

        @JsonProperty("description")
        private String description;

        @JsonProperty("code")
        private TypedValue code;

        @JsonProperty("location")
        private Location location;

        public ASTError() {}

        public String getName() { return name; }
        public void setName(String name) { this.name = name; }

        public ASTErrorType getErrorType() { return errorType; }
        public void setErrorType(ASTErrorType errorType) { this.errorType = errorType; }

        public String getDescription() { return description; }
        public void setDescription(String description) { this.description = description; }

        public TypedValue getCode() { return code; }
        public void setCode(TypedValue code) { this.code = code; }

        public Location getLocation() { return location; }
        public void setLocation(Location location) { this.location = location; }
    }

    @JsonInclude(JsonInclude.Include.NON_NULL)
    public static class ASTEndpoint {
        @JsonProperty("name")
        private String name;

        @JsonProperty("urn")
        private String urn;

        @JsonProperty("endpointType")
        private EndpointType endpointType;

        @JsonProperty("input")
        private ASTRecord input;

        @JsonProperty("output")
        private ASTRecord output;

        @JsonProperty("examples")
        private List<String> examples;

        @JsonProperty("errors")
        private List<ASTError> errors;

        @JsonProperty("location")
        private Location location;

        public ASTEndpoint() {}

        public String getName() { return name; }
        public void setName(String name) { this.name = name; }

        public String getUrn() { return urn; }
        public void setUrn(String urn) { this.urn = urn; }

        public EndpointType getEndpointType() { return endpointType; }
        public void setEndpointType(EndpointType endpointType) { this.endpointType = endpointType; }

        public ASTRecord getInput() { return input; }
        public void setInput(ASTRecord input) { this.input = input; }

        public ASTRecord getOutput() { return output; }
        public void setOutput(ASTRecord output) { this.output = output; }

        public List<String> getExamples() { return examples; }
        public void setExamples(List<String> examples) { this.examples = examples; }

        public List<ASTError> getErrors() { return errors; }
        public void setErrors(List<ASTError> errors) { this.errors = errors; }

        public Location getLocation() { return location; }
        public void setLocation(Location location) { this.location = location; }
    }
}
