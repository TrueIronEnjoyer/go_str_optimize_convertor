# AST String Concatenation Optimizer for Go

## Overview
This Go tool utilizes AST transformation to replace inefficient string concatenation (`+=`) with optimized constructs using `strings.Builder` and chunked processing, reducing memory allocations and boosting runtime performance.

## Features
- **Automatic Replacement:** Detects inefficient concatenation patterns and replaces them automatically.
- **Support for Both Constant and Dynamic Strings:** Implements chunked concatenation for constant strings and `strings.Builder` for dynamic ones.
- **Easy Integration:** Can be easily added to your Go projects to improve performance.

## Requirements
- Go 1.x or higher
- [golang.org/x/tools/go/ast/astutil](https://pkg.go.dev/golang.org/x/tools/go/ast/astutil)

## Installation and Usage
1. **Clone the Repository:**
   ```bash
   git clone https://github.com/your_username/ast-string-converter.git
   cd ast-string-converter
   ```
2. **Run the Converter:**
   ```bash
   go run main.go [path to file or directory]
   ```
   **Examples:**
   ```bash
   go run main.go ./example.go
   ```
   or
   ```bash
   go run main.go ./src
   ```

## How It Works
The tool scans Go source files, identifying and transforming inefficient string concatenation expressions into optimized code using `strings.Builder` and chunked processing. The transformed code replaces the original, so it is recommended to back up your files before running the converter.

## Project Structure
- **main.go** – Entry point for scanning files and transforming AST.
- Additional files include helper functions for AST manipulation, chunk splitting, and code generation.

## Contribution
Contributions, pull requests, and suggestions are welcome. Please follow the standard GitHub workflow for contributions.

## License
This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
