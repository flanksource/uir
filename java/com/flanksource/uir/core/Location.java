package com.flanksource.uir.core;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonProperty;

/**
 * Represents a file location with line range
 */
@JsonInclude(JsonInclude.Include.NON_NULL)
public class Location {
    @JsonProperty("path")
    private String path;

    @JsonProperty("start_line")
    private Integer startLine;

    @JsonProperty("end_line")
    private Integer endLine;

    public Location() {}

    public Location(String path, Integer startLine, Integer endLine) {
        this.path = path;
        this.startLine = startLine;
        this.endLine = endLine;
    }

    public String getPath() { return path; }
    public void setPath(String path) { this.path = path; }

    public Integer getStartLine() { return startLine; }
    public void setStartLine(Integer startLine) { this.startLine = startLine; }

    public Integer getEndLine() { return endLine; }
    public void setEndLine(Integer endLine) { this.endLine = endLine; }
}
