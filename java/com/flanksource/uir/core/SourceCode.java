package com.flanksource.uir.core;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.annotation.JsonUnwrapped;

/**
 * Represents source code with location and content
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class SourceCode {
    @JsonUnwrapped
    private Location location;

    @JsonProperty("content")
    private String content;

    @JsonProperty("language")
    private String language;

    public SourceCode() {}

    public SourceCode(Location location, String content, String language) {
        this.location = location;
        this.content = content;
        this.language = language;
    }

    public Location getLocation() { return location; }
    public void setLocation(Location location) { this.location = location; }

    public String getContent() { return content; }
    public void setContent(String content) { this.content = content; }

    public String getLanguage() { return language; }
    public void setLanguage(String language) { this.language = language; }
}
