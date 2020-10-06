// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

func main() {
	http.HandleFunc("/", home)
	http.HandleFunc("/curl", curl)
	port := "8080"
	if v := os.Getenv("PORT"); v != "" {
		port = v
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func home(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, `<!DOCTYPE html>
	<html lang="en">
		<head>
		<title>DNS Service Discovery for Cloud Run</title>
		<style>
			code { background-color: #dedede; }
		</style>
		</head>
		<body>
			<h1>Service Discovery + Auto-Authentication for Cloud Run</h1>
			<p>
				This Cloud Run fully-managed app queries other Cloud Run apps in the same project<br/>
				directly by name (<b>DNS service discovery</b>, just like Kubernetes)<br/>
				and you no longer need to add an <code>Authorization</code> header<br/>
				to your outbound requests by fetching an <em>identity token</em><br/>
				(Authorization header is automatically injected!).
			</p>
			<p>
				So you just query <code>http://SERVICE_NAME</code> and it can now find and<br/>
				authenticate out-of-the-box on Cloud Run!
			</p>
			<form method=POST action=/curl>
				URL: <input type="text" name="url" placeholder="http://hello" required size="60"/>
				<input type=submit value="Go"/>
			</form>
				<p>Try one of these:
					<ul>
						<li><code>http://ktest/req</code>: a public Service in the same region (us-central1)</li>
						<li><code>http://hello-private.asia-east1</code>: a private Service in another region, but same project</li>
						<li><code>http://hello-private.asia-east1.cloudrun.internal</code>: an internal FQDN</li>
					</ul>
				</p>
			<hr/>
			<p>
				All you need to do is to wrap your original entrypoint with a new process:
				<pre>
# Dockerfile
ENTRYPOINT ["<b>/runsd</b>", "--", "python3", "server.py"]</pre>
			<p>
				No new IAM permissions or code change is necessary!<br/>
				
			</p><p>
				<a href="http://doc/1lvC0Yy6AChCk09sp93PIrVM1wDWZWng0CJd7Z86yl2c">Design doc</a>
				&mdash; Contact:<a href="http://who/ahmetb/">ahmetb@</a>. 
			</p>
		</body>
	</html>
`)
}

func curl(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		fmt.Fprintf(w, "method %q not allowed", req.Method)
		return
	}
	url := req.PostFormValue("url")
	if url == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "url input field empty")
		return
	}
	if !strings.HasPrefix(url, "http") {
		url = "http://" + url
	}

	w.Header().Set("content-type", "text/plain; charset=UTF-8")

	fmt.Fprintf(w, "$ curl -sSLv %s\n\n", url)
	cmd := exec.Command("curl", "-sSLv", url)
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(w, "curl failed: %v\n", err)
	}
}
