# Release Process

etcd-manager is released on an as-needed basis.

## Check builds OK

Ensure the commit you are tagging is showing as green in github & prow test results.

## Tag the release

Pull the latest changes:
```
git checkout main
git pull upstream main
```

Set the version using `dev/set-version.sh`:
```
dev/set-version.sh
VERSION="$(cat version.txt)"
```

Create the branch and commit the changes (without pushing):
```
git checkout -b release_${VERSION}
git add version.txt && git commit -m "Release v${VERSION}"
```

This is the "release commit". Push and create a PR.
```
gh pr create -f -l tide/merge-method-squash
```


## Wait for staging job to complete

The staging job should now see the tag, and build it (from the trusted prow cluster, using Google Cloud Build).

The job is here: https://testgrid.k8s.io/sig-etcd-etcd-manager-postsubmit-push-to-staging

It (currently) takes about 10 minutes to run.

In the meantime, you can compile the release notes...

## Propose promotion of artifacts

The following tool is a prerequisite:

* [`kpromo`](https://github.com/kubernetes-sigs/promo-tools)

Create container promotion PR:

```
# Should show image tags
crane ls us-central1-docker.pkg.dev/k8s-staging-images/etcd-manager/etcd-manager-slim | grep "${VERSION}"
```

```
cd ../k8s.io

git checkout main
git pull
git checkout -b etcd-manager_images_${VERSION}

echo "# ${VERSION}" >> registry.k8s.io/images/k8s-staging-etcd-manager/images.yaml
kpromo cip --snapshot us-central1-docker.pkg.dev/k8s-staging-images/etcd-manager --snapshot-tag "v${VERSION}" >> registry.k8s.io/images/k8s-staging-etcd-manager/images.yaml
```

You can dry-run the promotion with

```
kpromo cip --thin-manifest-dir k8s.gcr.io
```

Send the image promotion PR:

```
git add -p registry.k8s.io/images/k8s-staging-etcd-manager/images.yaml
git commit -m "Promote etcd-manager ${VERSION} images"
gh pr create --fill --base main --repo kubernetes/k8s.io
```


## Smoketesting the release

More process coming soon, but in the meantime override the version
in a kOps cluster and validate.  Send the PR to kOps development branch and
let it go through e2e-tests before cherry-picking it.

## On github

* Add release notes
* Publish it
