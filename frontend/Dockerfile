# syntax=docker/dockerfile:1

FROM node:26-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

# `make test` builds this stage: typecheck, unit tests, and the raw-HTML guard.
FROM deps AS test
COPY . .
RUN npm run typecheck && npm test && npm run check:html

FROM deps AS build
COPY . .
RUN npm run build

FROM nginx:1.30-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
