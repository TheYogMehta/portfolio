## Prerequisites & Installation

Before running the project, make sure you have the following installed:

- **Go** (v1.22 or higher)
- **Node.js** (v18 or higher)
- **Templ CLI** (Component compiler for Go)
- **Air** (Live reloader for Go)

### Installing Go CLI Tools

Install the `templ` and `air` binaries globally:

```bash
go install github.com/a-h/templ/cmd/templ@latest
go install github.com/air-verse/air@latest
```

---

## Getting Started

### 1. Install Project Dependencies

```bash
# Install Node dependencies
npm install

# Download Go module dependencies
go mod download
```

### 2. Configure Environment Variables

Copy the example environment file:

```bash
cp .env.example .env
```

Update your `.env`

---

## Running in Development Mode

To start Templ generation, Tailwind CSS compilation, and Go server live reload together, run:

```bash
npm run dev
```

### Individual Development Commands

- **Generate Templ components continuously**:
  ```bash
  npm run dev:templ
  ```
- **Watch & compile Tailwind CSS**:
  ```bash
  npm run dev:css
  ```
- **Live reload Go server**:
  ```bash
  npm run dev:go
  ```

---

## Building for Production

To build all assets and compile the standalone production server binary:

```bash
npm run build
```

Run the compiled server:

```bash
./bin/server
```
